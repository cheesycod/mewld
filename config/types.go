package config

type CoreConfig struct {
	Dir         string   `yaml:"dir"`
	OverrideDir string   `yaml:"override_dir"`
	Env         []string `yaml:"env"`
	Names       string   `yaml:"names"`
}
