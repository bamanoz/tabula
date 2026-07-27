package hostcmd

import (
	"io"
	"path/filepath"

	"github.com/bamanoz/tabula/internal/logging"
)

func runtimeLoggingConfig(home string, stderr io.Writer) logging.Config {
	return logging.ApplyEnv(logging.Config{
		Component:     "runtime",
		ConsoleLevel:  "info",
		ConsoleFormat: "json",
		ConsoleWriter: stderr,
		FilePath:      filepath.Join(home, "logs", "runtime.log"),
		FileLevel:     "silent",
		FileFormat:    "json",
		Compress:      true,
	}, "TABULA_RUNTIME")
}

func setupRuntimeLogger(cfg logging.Config) *logging.Logger {
	return logging.Setup(cfg)
}
