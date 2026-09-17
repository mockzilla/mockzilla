package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/stretchr/testify/assert"
)

const fakeDriver = "fake-install"

func init() {
	Register(fakeDriver, func(options map[string]any) (Storage, error) {
		return options["storage"].(Storage), nil
	})
}

type fakeStorage struct {
	closed int
}

func (s *fakeStorage) NewDB(string, time.Duration) DB { return nil }
func (s *fakeStorage) Close()                         { s.closed++ }

type fakeInstaller struct {
	fakeStorage
	installs   int
	verifies   int
	installErr error
	verifyErr  error
}

func (s *fakeInstaller) Install(context.Context) error {
	s.installs++
	return s.installErr
}

func (s *fakeInstaller) VerifyInstall(context.Context) error {
	s.verifies++
	return s.verifyErr
}

func fakeConfig(storage Storage) *config.StorageConfig {
	return &config.StorageConfig{
		Type:         fakeDriver,
		DriverConfig: map[string]any{"storage": storage},
	}
}

func TestNewStorage(t *testing.T) {
	t.Run("nil config returns memory storage", func(t *testing.T) {
		storage := NewStorage(nil)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("empty type returns memory storage", func(t *testing.T) {
		cfg := &config.StorageConfig{Type: ""}
		storage := NewStorage(cfg)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("memory type returns memory storage", func(t *testing.T) {
		cfg := &config.StorageConfig{Type: config.StorageTypeMemory}
		storage := NewStorage(cfg)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("unknown type falls back to memory", func(t *testing.T) {
		cfg := &config.StorageConfig{Type: "unknown"}
		storage := NewStorage(cfg)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("redis with invalid config falls back to memory", func(t *testing.T) {
		cfg := &config.StorageConfig{
			Type:  config.StorageTypeRedis,
			Redis: &config.RedisConfig{Address: "invalid:99999"},
		}
		storage := NewStorage(cfg)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("redis via driver config with invalid config falls back to memory", func(t *testing.T) {
		cfg := &config.StorageConfig{
			Type: config.StorageTypeRedis,
			DriverConfig: map[string]any{
				"address": "invalid:99999",
			},
		}
		storage := NewStorage(cfg)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("strict unknown type panics instead of falling back", func(t *testing.T) {
		cfg := &config.StorageConfig{Type: "unknown", Strict: true}
		assert.PanicsWithValue(t, "storage is strict and could not be opened: unknown storage type: unknown", func() {
			NewStorage(cfg)
		})
	})

	t.Run("strict redis with invalid config panics instead of falling back", func(t *testing.T) {
		cfg := &config.StorageConfig{
			Type:   config.StorageTypeRedis,
			Redis:  &config.RedisConfig{Address: "invalid:99999"},
			Strict: true,
		}
		assert.Panics(t, func() { NewStorage(cfg) })
	})

	t.Run("strict memory type opens normally", func(t *testing.T) {
		storage := NewStorage(&config.StorageConfig{Type: config.StorageTypeMemory, Strict: true})
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("failed install falls back to memory", func(t *testing.T) {
		fake := &fakeInstaller{installErr: errors.New("no ddl rights")}
		storage := NewStorage(fakeConfig(fake))
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		assert.Equal(t, 1, fake.closed)
		storage.Close()
	})

	t.Run("strict failed install panics instead of falling back", func(t *testing.T) {
		cfg := fakeConfig(&fakeInstaller{installErr: errors.New("no ddl rights")})
		cfg.Strict = true
		assert.PanicsWithValue(t,
			"storage is strict and could not be opened: installing fake-install storage: no ddl rights",
			func() { NewStorage(cfg) })
	})
}

func TestOpenStorage(t *testing.T) {
	t.Run("nil config opens memory storage", func(t *testing.T) {
		storage, err := OpenStorage(nil)
		assert.NoError(t, err)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("unknown type returns an error", func(t *testing.T) {
		storage, err := OpenStorage(&config.StorageConfig{Type: "unknown"})
		assert.Nil(t, storage)
		assert.ErrorIs(t, err, ErrUnknownStorageType)
	})

	t.Run("driver failure returns an error", func(t *testing.T) {
		cfg := &config.StorageConfig{
			Type:  config.StorageTypeRedis,
			Redis: &config.RedisConfig{Address: "invalid:99999"},
		}
		storage, err := OpenStorage(cfg)
		assert.Nil(t, storage)
		assert.ErrorContains(t, err, "opening redis storage")
	})

	t.Run("installs by default", func(t *testing.T) {
		fake := &fakeInstaller{}
		storage, err := OpenStorage(fakeConfig(fake))
		assert.NoError(t, err)
		assert.Same(t, fake, storage)
		assert.Equal(t, 1, fake.installs)
		assert.Equal(t, 0, fake.verifies)
		assert.Equal(t, 0, fake.closed)
	})

	t.Run("only verifies when install is off", func(t *testing.T) {
		fake := &fakeInstaller{}
		cfg := fakeConfig(fake)
		cfg.Install = new(bool)
		storage, err := OpenStorage(cfg)
		assert.NoError(t, err)
		assert.Same(t, fake, storage)
		assert.Equal(t, 0, fake.installs)
		assert.Equal(t, 1, fake.verifies)
	})

	t.Run("install failure closes the storage and returns an error", func(t *testing.T) {
		cause := errors.New("no ddl rights")
		fake := &fakeInstaller{installErr: cause}
		storage, err := OpenStorage(fakeConfig(fake))
		assert.Nil(t, storage)
		assert.ErrorIs(t, err, cause)
		assert.ErrorContains(t, err, "installing fake-install storage")
		assert.Equal(t, 1, fake.closed)
	})

	t.Run("verify failure closes the storage and returns an error", func(t *testing.T) {
		cause := errors.New("schema is not installed")
		fake := &fakeInstaller{verifyErr: cause}
		cfg := fakeConfig(fake)
		cfg.Install = new(bool)
		storage, err := OpenStorage(cfg)
		assert.Nil(t, storage)
		assert.ErrorIs(t, err, cause)
		assert.ErrorContains(t, err, "verifying fake-install storage install")
		assert.Equal(t, 1, fake.closed)
	})

	t.Run("storage without an installer is left alone", func(t *testing.T) {
		fake := &fakeStorage{}
		storage, err := OpenStorage(fakeConfig(fake))
		assert.NoError(t, err)
		assert.Same(t, fake, storage)
		assert.Equal(t, 0, fake.closed)
	})
}

func TestStorageInterface(t *testing.T) {
	var _ Storage = (*memoryStorage)(nil)
	var _ Storage = (*redisStorage)(nil)
}
