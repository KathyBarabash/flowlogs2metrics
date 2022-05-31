/*
 * Copyright (C) 2021 IBM, Inc.
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

package extract

import (
	"github.com/netobserv/flowlogs-pipeline/pkg/api"
	"github.com/netobserv/flowlogs-pipeline/pkg/config"
	"github.com/netobserv/flowlogs-pipeline/pkg/pipeline/extract/aggregate"
	log "github.com/sirupsen/logrus"
)

type ExtractAggregate struct {
	Aggregates aggregate.Aggregates
}

// Extract extracts a flow before being stored
func (ea *ExtractAggregate) Extract(entries []config.GenericMap) []config.GenericMap {
	err := ea.Aggregates.Evaluate(entries)
	if err != nil {
		log.Debugf("Evaluate error %v", err)
	}

	// TODO: This need to be async function that is being called for the metrics and not
	// TODO: synchronized from the pipeline directly.
	return ea.Aggregates.GetMetrics()
}

// NewExtractAggregate creates a new extractor
func NewExtractAggregate(params config.StageParam) (Extractor, error) {
	log.Debugf("entering NewExtractAggregate")
	aggConfig := []api.AggregateDefinition{}
	if params.Extract != nil {
		aggConfig = params.Extract.Aggregates
	}
	aggregates, err := aggregate.NewAggregatesFromConfig(aggConfig)
	if err != nil {
		log.Errorf("error in NewAggregatesFromConfig: %v", err)
		return nil, err
	}

	return &ExtractAggregate{
		Aggregates: aggregates,
	}, nil
}
