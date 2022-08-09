package config

type oauth struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret" json:"-"`
	RedirectURL  string `yaml:"redirect_url"`
}

type CoreConfig struct {
	Dir          string   `yaml:"dir"`
	OverrideDir  string   `yaml:"override_dir"`
	Env          []string `yaml:"env"`
	Names        string   `yaml:"names"`
	Module       string   `yaml:"module"`
	Redis        string   `yaml:"redis"`
	RedisChannel string   `yaml:"redis_channel"`
	Interp       string   `yaml:"interp"`
	AllowedIDS   []string `yaml:"allowed_ids"`
	Oauth        oauth    `yaml:"oauth"`
	PingInterval int      `yaml:"ping_interval"`
}
