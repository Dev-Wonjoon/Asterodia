package desktop

import (
	"context"
	"sync"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/database"
)

type Service struct {
	mu             sync.Mutex
	importer       *database.Importer
	selectDatabase func() (string, error)
}

func NewService(importer *database.Importer, selectDatabase func() (string, error)) *Service {
	return &Service{importer: importer, selectDatabase: selectDatabase}
}
func (s *Service) ImportDatabase(ctx context.Context) api.Result {
	if !s.mu.TryLock() {
		return api.Failure("DATABASE_IMPORT_FAILED", "다른 DB 가져오기가 진행 중입니다")
	}
	defer s.mu.Unlock()
	if s.selectDatabase == nil {
		return api.Failure("DATABASE_IMPORT_FAILED", "DB 선택 창을 사용할 수 없습니다")
	}
	path, err := s.selectDatabase()
	if err != nil {
		return api.Failure("DATABASE_IMPORT_FAILED", err.Error())
	}
	if path == "" {
		return api.Success(database.ImportResult{Cancelled: true})
	}
	result, err := s.importer.Import(ctx, path)
	if err != nil {
		return api.Failure("DATABASE_IMPORT_FAILED", err.Error())
	}
	return api.Success(result)
}
