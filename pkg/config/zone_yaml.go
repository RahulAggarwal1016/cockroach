// Copyright 2018 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package config

import (
	"fmt"
	"runtime/debug"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v2"
)

var _ yaml.Marshaler = LeasePreference{}
var _ yaml.Unmarshaler = &LeasePreference{}

// MarshalYAML implements yaml.Marshaler.
func (l LeasePreference) MarshalYAML() (interface{}, error) {
	short := make([]string, len(l.Constraints))
	for i, c := range l.Constraints {
		short[i] = c.String()
	}
	return short, nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (l *LeasePreference) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var shortConstraints []string
	if err := unmarshal(&shortConstraints); err != nil {
		return err
	}
	constraints := make([]Constraint, len(shortConstraints))
	for i, short := range shortConstraints {
		if err := constraints[i].FromString(short); err != nil {
			return err
		}
	}
	l.Constraints = constraints
	return nil
}

var _ yaml.Marshaler = Constraints{}
var _ yaml.Unmarshaler = &Constraints{}

// MarshalYAML implements yaml.Marshaler.
func (c Constraints) MarshalYAML() (interface{}, error) {
	return nil, fmt.Errorf(
		"MarshalYAML should never be called directly on Constraints (%v): %v", c, debug.Stack())
}

// UnmarshalYAML implements yaml.Marshaler.
func (c *Constraints) UnmarshalYAML(unmarshal func(interface{}) error) error {
	return fmt.Errorf(
		"UnmarshalYAML should never be called directly on Constraints: %v", debug.Stack())
}

// ConstraintsList is an alias for a slice of Constraints that can be
// properly marshaled to/from YAML.
type ConstraintsList []Constraints

var _ yaml.Marshaler = ConstraintsList{}
var _ yaml.Unmarshaler = &ConstraintsList{}

// MarshalYAML implements yaml.Marshaler.
//
// We use two different formats here, dependent on whether per-replica
// constraints are being used in ConstraintsList:
// 1. A legacy format when there are 0 or 1 Constraints and NumReplicas is
//    zero:
//    [c1, c2, c3]
// 2. A per-replica format when NumReplicas is non-zero:
//    {"c1,c2,c3": numReplicas1, "c4,c5": numReplicas2}
func (c ConstraintsList) MarshalYAML() (interface{}, error) {
	// If per-replica Constraints aren't in use, marshal everything into a list
	// for compatibility with pre-2.0-style configs.
	if len(c) == 0 {
		return []string{}, nil
	}
	if len(c) == 1 && c[0].NumReplicas == 0 {
		short := make([]string, len(c[0].Constraints))
		for i, constraint := range c[0].Constraints {
			short[i] = constraint.String()
		}
		return short, nil
	}

	// Otherwise, convert into a map from Constraints to NumReplicas.
	constraintsMap := make(map[string]int32)
	for _, constraints := range c {
		short := make([]string, len(constraints.Constraints))
		for i, constraint := range constraints.Constraints {
			short[i] = constraint.String()
		}
		constraintsMap[strings.Join(short, ",")] = constraints.NumReplicas
	}
	return constraintsMap, nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (c *ConstraintsList) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Note that we're intentionally checking for err == nil here. This handles
	// unmarshaling the legacy Constraints format, which is just a list of
	// strings.
	var strs []string
	if err := unmarshal(&strs); err == nil {
		constraints := make([]Constraint, len(strs))
		for i, short := range strs {
			if err := constraints[i].FromString(short); err != nil {
				return err
			}
		}
		if len(constraints) == 0 {
			*c = []Constraints{}
		} else {
			*c = []Constraints{
				{
					Constraints: constraints,
					NumReplicas: 0,
				},
			}
		}
		return nil
	}

	// Otherwise, the input must be a map that can be converted to per-replica
	// constraints.
	constraintsMap := make(map[string]int32)
	if err := unmarshal(&constraintsMap); err != nil {
		return err
	}

	constraintsList := make([]Constraints, 0, len(constraintsMap))
	for constraintsStr, numReplicas := range constraintsMap {
		shortConstraints := strings.Split(constraintsStr, ",")
		constraints := make([]Constraint, len(shortConstraints))
		for i, short := range shortConstraints {
			if err := constraints[i].FromString(short); err != nil {
				return err
			}
		}
		constraintsList = append(constraintsList, Constraints{
			Constraints: constraints,
			NumReplicas: numReplicas,
		})
	}

	// Sort the resulting list for reproducible orderings in tests.
	sort.Slice(constraintsList, func(i, j int) bool {
		// First, try to find which Constraints sort first alphabetically in string
		// format, considering the shorter list lesser if they're otherwise equal.
		for k := range constraintsList[i].Constraints {
			if k >= len(constraintsList[j].Constraints) {
				return false
			}
			lStr := constraintsList[i].Constraints[k].String()
			rStr := constraintsList[j].Constraints[k].String()
			if lStr < rStr {
				return true
			}
			if lStr > rStr {
				return false
			}
		}
		if len(constraintsList[i].Constraints) < len(constraintsList[j].Constraints) {
			return true
		}
		// If they're completely equal and the same length, go by NumReplicas.
		return constraintsList[i].NumReplicas < constraintsList[j].NumReplicas
	})

	*c = constraintsList
	return nil
}

// marshalableZoneConfig should be kept up-to-date with the real,
// auto-generated ZoneConfig type, but with []Constraints changed to
// ConstraintsList for backwards-compatible yaml marshaling and unmarshaling.
// We also support parsing both lease_preferences (for v2.1+) and
// experimental_lease_preferences (for v2.0), copying both into the same proto
// field as needed.
//
// TODO(a-robinson,v2.2): Remove the experimental_lease_preferences field.
type marshalableZoneConfig struct {
	RangeMinBytes                int64             `json:"range_min_bytes" yaml:"range_min_bytes"`
	RangeMaxBytes                int64             `json:"range_max_bytes" yaml:"range_max_bytes"`
	GC                           GCPolicy          `json:"gc"`
	NumReplicas                  int32             `json:"num_replicas" yaml:"num_replicas"`
	Constraints                  ConstraintsList   `json:"constraints" yaml:"constraints,flow"`
	LeasePreferences             []LeasePreference `json:"lease_preferences" yaml:"lease_preferences,flow"`
	ExperimentalLeasePreferences []LeasePreference `json:"experimental_lease_preferences" yaml:"experimental_lease_preferences,flow,omitempty"`
	Subzones                     []Subzone         `json:"subzones" yaml:"-"`
	SubzoneSpans                 []SubzoneSpan     `json:"subzone_spans" yaml:"-"`
}

func zoneConfigToMarshalable(c ZoneConfig) marshalableZoneConfig {
	var m marshalableZoneConfig
	m.RangeMinBytes = c.RangeMinBytes
	m.RangeMaxBytes = c.RangeMaxBytes
	m.GC = c.GC
	if c.NumReplicas != 0 {
		m.NumReplicas = c.NumReplicas
	}
	m.Constraints = ConstraintsList(c.Constraints)
	m.LeasePreferences = c.LeasePreferences
	// We intentionally do not round-trip ExperimentalLeasePreferences. We never
	// want to return yaml containing it.
	m.Subzones = c.Subzones
	m.SubzoneSpans = c.SubzoneSpans
	return m
}

func zoneConfigFromMarshalable(m marshalableZoneConfig) ZoneConfig {
	var c ZoneConfig
	c.RangeMinBytes = m.RangeMinBytes
	c.RangeMaxBytes = m.RangeMaxBytes
	c.GC = m.GC
	c.NumReplicas = m.NumReplicas
	c.Constraints = []Constraints(m.Constraints)
	c.LeasePreferences = m.LeasePreferences
	// Prefer a provided m.ExperimentalLeasePreferences value over whatever is in
	// m.LeasePreferences, since we know that m.ExperimentalLeasePreferences can
	// only possibly come from the user-specified input, whereas
	// m.LeasePreferences could be the old value of the field retrieved from
	// internal storage that the user is now trying to overwrite.
	if m.ExperimentalLeasePreferences != nil {
		c.LeasePreferences = m.ExperimentalLeasePreferences
	}
	c.Subzones = m.Subzones
	c.SubzoneSpans = m.SubzoneSpans
	return c
}

var _ yaml.Marshaler = ZoneConfig{}
var _ yaml.Unmarshaler = &ZoneConfig{}

// MarshalYAML implements yaml.Marshaler.
func (c ZoneConfig) MarshalYAML() (interface{}, error) {
	return zoneConfigToMarshalable(c), nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (c *ZoneConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Pre-initialize aux with the contents of c. This is important for
	// maintaining the behavior of not overwriting existing fields unless the
	// user provided new values for them.
	aux := zoneConfigToMarshalable(*c)
	if err := unmarshal(&aux); err != nil {
		return err
	}
	*c = zoneConfigFromMarshalable(aux)
	return nil
}
