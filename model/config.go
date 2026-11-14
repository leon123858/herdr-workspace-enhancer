package model

// PluginConfig holds preferences shared by all sessions of this plugin.
type PluginConfig struct {
	TUI TUIConfig `mapstructure:"tui"`
}
type TUIConfig struct {
	WorkspaceRatio float64 `mapstructure:"workspace_ratio"`
}

const DefaultWorkspaceRatio = 0.35
