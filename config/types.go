package config

type CoreConfig struct {
	Dir          string   `yaml:"dir"`
	OverrideDir  string   `yaml:"override_dir"`
	Env          []string `yaml:"env"`
	Names        string   `yaml:"names"`
	Module       string   `yaml:"module"`
	Redis        string   `yaml:"redis"`
	RedisChannel string   `yaml:"redis_channel"`
	Interp       string   `yaml:"interp"`
}
