package refine

import (
	"fmt"

	"github.com/samahlstrom/forge-cli/internal/util"
)

// Criteria defines what "better" means for a refine loop.
type Criteria struct {
	Measure  string   `yaml:"measure"`
	Metrics  []Metric `yaml:"metrics"`
	Primary  string   `yaml:"primary"`
	Objective string  `yaml:"objective"`
	Scope    Scope    `yaml:"scope"`

	MaxIterations int `yaml:"max_iterations"`
	IdleTimeout   int `yaml:"idle_timeout"` // seconds

	StopWhen StopConditions `yaml:"stop_when"`
}

type Metric struct {
	Name      string  `yaml:"name"`
	Direction string  `yaml:"direction"` // "minimize" or "maximize"
	Target    float64 `yaml:"target"`
	HasTarget bool    `yaml:"-"`
}

type Scope struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

type StopConditions struct {
	AllTargetsMet    bool `yaml:"all_targets_met"`
	NoImprovementFor int  `yaml:"no_improvement_for"`
}

func ParseCriteria(path string) (*Criteria, error) {
	var c Criteria
	if err := util.ReadYAML(path, &c); err != nil {
		return nil, fmt.Errorf("parse criteria: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Criteria) validate() error {
	if c.Measure == "" {
		return fmt.Errorf("criteria: 'measure' command is required")
	}
	if len(c.Metrics) == 0 {
		return fmt.Errorf("criteria: at least one metric is required")
	}
	if c.Primary == "" {
		c.Primary = c.Metrics[0].Name
	}

	foundPrimary := false
	for i := range c.Metrics {
		m := &c.Metrics[i]
		if m.Name == "" {
			return fmt.Errorf("criteria: metric at index %d has no name", i)
		}
		if m.Direction != "minimize" && m.Direction != "maximize" {
			return fmt.Errorf("criteria: metric %q direction must be 'minimize' or 'maximize', got %q", m.Name, m.Direction)
		}
		if m.Target != 0 {
			m.HasTarget = true
		}
		if m.Name == c.Primary {
			foundPrimary = true
		}
	}
	if !foundPrimary {
		return fmt.Errorf("criteria: primary metric %q not found in metrics list", c.Primary)
	}

	if c.MaxIterations <= 0 {
		c.MaxIterations = 25
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 600
	}
	if c.StopWhen.NoImprovementFor <= 0 {
		c.StopWhen.NoImprovementFor = 5
	}

	return nil
}

// PrimaryMetric returns the metric definition for the primary metric.
func (c *Criteria) PrimaryMetric() Metric {
	for _, m := range c.Metrics {
		if m.Name == c.Primary {
			return m
		}
	}
	return c.Metrics[0]
}
