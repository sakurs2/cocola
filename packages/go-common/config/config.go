// Package config holds shared configuration primitives. M0 only defines the
// envelope; concrete loaders (env, file, K8s ConfigMap) land in M3.
package config

// Env represents the runtime environment classification.
type Env string

const (
	EnvDev     Env = "dev"
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
)

// Common is embedded by every service-specific config struct.
type Common struct {
	Env         Env    `yaml:"env"          env:"COCOLA_ENV"          envDefault:"dev"`
	ServiceName string `yaml:"service_name" env:"COCOLA_SERVICE_NAME"`
	LogLevel    string `yaml:"log_level"    env:"COCOLA_LOG_LEVEL"    envDefault:"info"`
}
