package configs

import "fmt"

type RedisConfig struct {
	Host         string `env:"HOST" envDefault:"localhost"`
	Port         int    `env:"PORT" envDefault:"6379"`
	Password     string `env:"PASSWORD" envDefault:""`
	DB           int    `env:"DB" envDefault:"0"`
	PoolSize     int    `env:"POOL_SIZE" envDefault:"10"`
	MinIdleConns int    `env:"MIN_IDLE_CONNS" envDefault:"5"`
}

func (r RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}
