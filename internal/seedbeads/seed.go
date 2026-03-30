package seedbeads

import (
	"fmt"
	"github.com/samahlstrom/forge-cli/internal/bd"
	"github.com/samahlstrom/forge-cli/internal/util"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type specTask struct {
	ID           string   `yaml:"id"`
	Title        string   `yaml:"title"`
	Description  string   `yaml:"description"`
	RiskTier     string   `yaml:"risk_tier"`
	Dependencies []string `yaml:"dependencies"`
	FilesLikely  []string `yaml:"files_likely"`
	Agent        string   `yaml:"agent"`
}

type specFeature struct {
	ID    string     `yaml:"id"`
	Title string     `yaml:"title"`
	Tasks []specTask `yaml:"tasks"`
}

type specEpic struct {
	ID       string        `yaml:"id"`
	Domain   string        `yaml:"domain"`
	Title    string        `yaml:"title"`
	Features []specFeature `yaml:"features"`
}

type specPhase struct {
	ID             string   `yaml:"id"`
	Name           string   `yaml:"name"`
	Epics          []string `yaml:"epics"`
	Rationale      string   `yaml:"rationale"`
	Parallelizable bool     `yaml:"parallelizable"`
}

type specYAML struct {
	SpecID  string     `yaml:"spec_id"`
	Status  string     `yaml:"status"`
	Summary string     `yaml:"summary"`
	Epics   []specEpic `yaml:"epics"`
	Plan    struct {
		Phases     []specPhase `yaml:"phases"`
		TotalTasks int         `yaml:"total_tasks"`
	} `yaml:"execution_plan"`
}

// Result holds the outcome of seeding beads.
type Result struct {
	Phases  int
	Epics   int
	Tasks   int
	Links   int
	TaskMap map[string]string // spec task id -> bd id
}

// SeedBeads creates bd issues from a spec.yaml decomposition.
func SeedBeads(specDir, specID, cwd string) (Result, error) {
	data, err := util.ReadText(filepath.Join(specDir, "spec.yaml"))
	if err != nil {
		return Result{}, err
	}

	var spec specYAML
	if err := yaml.Unmarshal([]byte(data), &spec); err != nil {
		return Result{}, err
	}

	taskMap := map[string]string{}
	epicMap := map[string]string{}
	var phaseEpicIDs []string
	linkCount := 0

	for pi, phase := range spec.Plan.Phases {
		phaseNum := pi + 1

		// Create phase epic
		phaseBeadID, err := bd.Create(bd.CreateOpts{
			Title:  sprintf("Phase %d: %s", phaseNum, phase.Name),
			Type:   "epic",
			Labels: []string{sprintf("spec:%s", specID), sprintf("phase:%d", phaseNum)},
			Metadata: map[string]any{
				"spec_phase_id": phase.ID,
				"rationale":     phase.Rationale,
			},
		}, cwd)
		if err != nil {
			return Result{}, err
		}
		phaseEpicIDs = append(phaseEpicIDs, phaseBeadID)

		// Phase ordering
		if pi > 0 {
			if err := bd.Link(phaseBeadID, phaseEpicIDs[pi-1], "blocks", cwd); err != nil {
				return Result{}, err
			}
			linkCount++
		}

		// Create epics within this phase
		for _, epicID := range phase.Epics {
			epic := findEpic(spec.Epics, epicID)
			if epic == nil {
				continue
			}

			epicBeadID, err := bd.Create(bd.CreateOpts{
				Title:  epic.Title,
				Type:   "epic",
				Parent: phaseBeadID,
				Labels: []string{
					sprintf("spec:%s", specID),
					sprintf("phase:%d", phaseNum),
					sprintf("domain:%s", epic.Domain),
				},
			}, cwd)
			if err != nil {
				return Result{}, err
			}
			epicMap[epic.ID] = epicBeadID

			// Create tasks
			for _, feature := range epic.Features {
				for _, task := range feature.Tasks {
					taskBeadID, err := bd.Create(bd.CreateOpts{
						Title:       task.Title,
						Description: task.Description,
						Parent:      epicBeadID,
						Labels: []string{
							sprintf("spec:%s", specID),
							sprintf("phase:%d", phaseNum),
							sprintf("tier:%s", task.RiskTier),
							sprintf("agent:%s", task.Agent),
							sprintf("feature:%s", feature.ID),
						},
						Metadata: map[string]any{
							"spec_task_id": task.ID,
							"files_likely": task.FilesLikely,
						},
					}, cwd)
					if err != nil {
						return Result{}, err
					}
					taskMap[task.ID] = taskBeadID
				}
			}
		}
	}

	// Wire up task-level dependencies
	for _, epic := range spec.Epics {
		for _, feature := range epic.Features {
			for _, task := range feature.Tasks {
				if len(task.Dependencies) == 0 {
					continue
				}
				thisID := taskMap[task.ID]
				if thisID == "" {
					continue
				}
				for _, depID := range task.Dependencies {
					depBeadID := taskMap[depID]
					if depBeadID == "" {
						continue
					}
					if err := bd.Link(thisID, depBeadID, "blocks", cwd); err != nil {
						continue
					}
					linkCount++
				}
			}
		}
	}

	return Result{
		Phases:  len(spec.Plan.Phases),
		Epics:   len(epicMap),
		Tasks:   len(taskMap),
		Links:   linkCount,
		TaskMap: taskMap,
	}, nil
}

func findEpic(epics []specEpic, id string) *specEpic {
	for i := range epics {
		if epics[i].ID == id {
			return &epics[i]
		}
	}
	return nil
}

func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
