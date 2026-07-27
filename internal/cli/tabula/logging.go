package tabula

import "github.com/bamanoz/tabula/internal/logging"

func setupKernelLogger(cfg logging.Config) *logging.Logger {
	return logging.Setup(cfg)
}
