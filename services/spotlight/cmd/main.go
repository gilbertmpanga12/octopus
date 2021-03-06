package main

import (
	"fmt"
	"os"

	"github.com/TruStory/octopus/services/spotlight"

	truCtx "github.com/TruStory/octopus/services/truapi/context"
)

func main() {
	port := getEnv("PORT", "54448")
	endpoint := mustEnv("SPOTLIGHT_GRAPHQL_ENDPOINT")
	jpegEnabled := getEnv("SPOTLIGHT_JPEG_ENABLED", "") == "true"
	config := truCtx.Config{
		Database: truCtx.DatabaseConfig{
			Host: getEnv("PG_ADDR", "localhost"),
			Port: 5432,
			User: getEnv("PG_USER", "postgres"),
			Pass: getEnv("PG_USER_PW", ""),
			Name: getEnv("PG_DB_NAME", "trudb"),
			Pool: 25,
		},
	}
	service := spotlight.NewService(port, endpoint, jpegEnabled, config)
	service.Run()
}
func getEnv(env, defaultValue string) string {
	val := os.Getenv(env)
	if val != "" {
		return val
	}
	return defaultValue
}

func mustEnv(env string) string {
	val := os.Getenv(env)
	if val == "" {
		panic(fmt.Sprintf("must provide %s variable", env))
	}
	return val
}
