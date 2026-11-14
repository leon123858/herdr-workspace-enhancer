package service

import (
	"fmt"
	"herdr-workspace-enhancer/model"
	"math"
)

type SettingsRepository interface {
	LoadRatio() (float64, error)
	SaveRatio(float64) error
}
type SettingsService struct{ Repository SettingsRepository }

func (s *SettingsService) Load() (model.PluginConfig, error) {
	r, e := s.Repository.LoadRatio()
	return model.PluginConfig{TUI: model.TUIConfig{WorkspaceRatio: r}}, e
}
func (s *SettingsService) LoadRatio() (float64, error) {
	c, e := s.Load()
	return c.TUI.WorkspaceRatio, e
}
func (s *SettingsService) SaveRatio(r float64) error {
	if math.IsNaN(r) || math.IsInf(r, 0) || r < .1 || r > .9 {
		return fmt.Errorf("workspace width ratio must be between 0.1 and 0.9")
	}
	return s.Repository.SaveRatio(r)
}
