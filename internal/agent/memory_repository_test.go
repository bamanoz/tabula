package agent_test

import (
	"testing"

	"github.com/bamanoz/tabula/internal/agent"
	"github.com/bamanoz/tabula/internal/agent/repositorytest"
)

func TestMemoryRepositoryConformance(t *testing.T) {
	repositorytest.Run(t, func(t *testing.T) agent.SessionRepository {
		t.Helper()
		return agent.NewMemoryRepository()
	})
}
