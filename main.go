package main

import (
	"github.com/OrcaCD/orca-cd/cmd"
	"github.com/OrcaCD/orca-cd/internal/utils"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	log.Logger = log.Logger.With().Caller().Logger()
	if !utils.ShoudLogJSON(os.Environ(), os.Args) {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}

	cmd.Run()
}
