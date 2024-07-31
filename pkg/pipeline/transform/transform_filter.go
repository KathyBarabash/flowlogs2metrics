/*
 * Copyright (C) 2022 IBM, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package transform

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"github.com/Knetic/govaluate"
	"github.com/netobserv/flowlogs-pipeline/pkg/api"
	"github.com/netobserv/flowlogs-pipeline/pkg/config"
	"github.com/netobserv/flowlogs-pipeline/pkg/utils"
	"github.com/sirupsen/logrus"
)

var (
	tlog   = logrus.WithField("component", "transform.Filter")
	rndgen = rand.New(rand.NewSource(time.Now().UnixNano()))
)

type Filter struct {
	Rules []api.TransformFilterRule
}

// Transform transforms a flow; if false is returned as a second argument, the entry is dropped
func (f *Filter) Transform(entry config.GenericMap) (config.GenericMap, bool) {
	tlog.Tracef("f = %v", f)
	outputEntry := entry.Copy()
	labels := make(map[string]string)
	for i := range f.Rules {
		tlog.Tracef("rule = %v", f.Rules[i])
		if cont := applyRule(outputEntry, labels, &f.Rules[i]); !cont {
			return nil, false
		}
	}
	// process accumulated labels into comma separated string
	if len(labels) > 0 {
		var sb strings.Builder
		for key, value := range labels {
			sb.WriteString(key)
			sb.WriteString("=")
			sb.WriteString(value)
			sb.WriteString(",")
		}
		// remove trailing comma
		labelsString := sb.String()
		labelsString = strings.TrimRight(labelsString, ",")
		outputEntry["labels"] = labelsString
	}
	return outputEntry, true
}

// Apply a rule. Returns false if it must stop processing rules (e.g. if entry must be removed)
// nolint:cyclop
func applyRule(entry config.GenericMap, labels map[string]string, rule *api.TransformFilterRule) bool {
	switch rule.Type {
	case api.RemoveField:
		delete(entry, rule.RemoveField.Input)
	case api.RemoveEntryIfExists:
		if _, ok := entry[rule.RemoveEntry.Input]; ok {
			return false
		}
	case api.RemoveEntryIfDoesntExist:
		if _, ok := entry[rule.RemoveEntry.Input]; !ok {
			return false
		}
	case api.RemoveEntryIfEqual:
		if val, ok := entry[rule.RemoveEntry.Input]; ok {
			if val == rule.RemoveEntry.Value {
				return false
			}
		}
	case api.RemoveEntryIfNotEqual:
		if val, ok := entry[rule.RemoveEntry.Input]; ok {
			if val != rule.RemoveEntry.Value {
				return false
			}
		}
	case api.AddField:
		entry[rule.AddField.Input] = rule.AddField.Value
	case api.AddFieldIfDoesntExist:
		if _, ok := entry[rule.AddFieldIfDoesntExist.Input]; !ok {
			entry[rule.AddFieldIfDoesntExist.Input] = rule.AddFieldIfDoesntExist.Value
		}
	case api.AddRegExIf:
		matched, err := regexp.MatchString(rule.AddRegExIf.Parameters, fmt.Sprintf("%s", entry[rule.AddRegExIf.Input]))
		if err != nil {
			return true
		}
		if matched {
			entry[rule.AddRegExIf.Output] = entry[rule.AddRegExIf.Input]
			entry[rule.AddRegExIf.Output+"_Matched"] = true
		}
	case api.AddFieldIf:
		expressionString := fmt.Sprintf("val %s", rule.AddFieldIf.Parameters)
		expression, err := govaluate.NewEvaluableExpression(expressionString)
		if err != nil {
			log.Warningf("Can't evaluate AddIf rule: %+v expression: %v. err %v", rule, expressionString, err)
			return true
		}
		result, evaluateErr := expression.Evaluate(map[string]interface{}{"val": entry[rule.AddFieldIf.Input]})
		if evaluateErr == nil && result.(bool) {
			if rule.AddFieldIf.Assignee != "" {
				entry[rule.AddFieldIf.Output] = rule.AddFieldIf.Assignee
			} else {
				entry[rule.AddFieldIf.Output] = entry[rule.AddFieldIf.Input]
			}
			entry[rule.AddFieldIf.Output+"_Evaluate"] = true
		}
	case api.AddLabel:
		labels[rule.AddLabel.Input] = utils.ConvertToString(rule.AddLabel.Value)
	case api.AddLabelIf:
		// TODO perhaps add a cache of previously evaluated expressions
		expressionString := fmt.Sprintf("val %s", rule.AddLabelIf.Parameters)
		expression, err := govaluate.NewEvaluableExpression(expressionString)
		if err != nil {
			log.Warningf("Can't evaluate AddLabelIf rule: %+v expression: %v. err %v", rule, expressionString, err)
			return true
		}
		result, evaluateErr := expression.Evaluate(map[string]interface{}{"val": entry[rule.AddLabelIf.Input]})
		if evaluateErr == nil && result.(bool) {
			labels[rule.AddLabelIf.Output] = rule.AddLabelIf.Assignee
		}
	case api.RemoveEntryAllSatisfied:
		return !isRemoveEntrySatisfied(entry, rule.RemoveEntryAllSatisfied)
	case api.ConditionalSampling:
		return sample(entry, rule.ConditionalSampling)
	default:
		tlog.Panicf("unknown type %s for transform.Filter rule: %v", rule.Type, rule)
	}
	return true
}

func isRemoveEntrySatisfied(entry config.GenericMap, rules []*api.RemoveEntryRule) bool {
	for _, r := range rules {
		// applyRule returns false if the entry must be removed
		if dontRemove := applyRule(entry, nil, &api.TransformFilterRule{Type: api.TransformFilterEnum(r.Type), RemoveEntry: r.RemoveEntry}); dontRemove {
			return false
		}
	}
	return true
}

func sample(entry config.GenericMap, rules []*api.SamplingCondition) bool {
	for _, r := range rules {
		if isRemoveEntrySatisfied(entry, r.Rules) {
			return r.Value == 0 || (rndgen.Intn(int(r.Value)) == 0)
		}
	}
	return true
}

// NewTransformFilter create a new filter transform
func NewTransformFilter(params config.StageParam) (Transformer, error) {
	tlog.Debugf("entering NewTransformFilter")
	rules := []api.TransformFilterRule{}
	if params.Transform != nil && params.Transform.Filter != nil {
		params.Transform.Filter.Preprocess()
		rules = params.Transform.Filter.Rules
	}
	transformFilter := &Filter{
		Rules: rules,
	}
	return transformFilter, nil
}
