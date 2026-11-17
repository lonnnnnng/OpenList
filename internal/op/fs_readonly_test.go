package op

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

type freshReadDriver struct {
	model.Storage
	driver.RootID
	list func(context.Context, model.Obj, model.ListArgs) ([]model.Obj, error)
}

func (d *freshReadDriver) Config() driver.Config          { return driver.Config{Name: "fixture"} }
func (d *freshReadDriver) GetAddition() driver.Additional { return &d.RootID }
func (d *freshReadDriver) Init(context.Context) error     { return nil }
func (d *freshReadDriver) Drop(context.Context) error     { return nil }
func (d *freshReadDriver) List(ctx context.Context, parent model.Obj, args model.ListArgs) ([]model.Obj, error) {
	return d.list(ctx, parent, args)
}
func (d *freshReadDriver) Link(context.Context, model.Obj, model.LinkArgs) (*model.Link, error) {
	return nil, errors.New("unused")
}

func freshFixture() *freshReadDriver {
	return &freshReadDriver{Storage: model.Storage{ID: 90, MountPath: "/fresh-fixture", Status: WORK}, RootID: driver.RootID{RootFolderID: "native-root"}}
}

func TestFreshReadIgnoresStaleAncestorCacheAndSharedFlight(t *testing.T) {
	d := freshFixture()
	oldCache := Cache
	Cache = NewCacheManager()
	t.Cleanup(func() { Cache = oldCache })
	oldParent := &model.Object{ID: "stale-parent", Name: "dir", IsFolder: true}
	Cache.dirCache.Set(Key(d, "/"), newDirectoryCache([]model.Obj{oldParent}))
	parent := &model.Object{ID: "current-parent", Name: "dir", IsFolder: true}
	file := &model.Object{ID: "native-file", Name: "电影 #%25.mkv"}
	calls := 0
	d.list = func(ctx context.Context, p model.Obj, args model.ListArgs) ([]model.Obj, error) {
		calls++
		if !args.Refresh || !args.SkipHook {
			t.Fatal("strict read allowed stale data or hooks")
		}
		switch p.GetID() {
		case "native-root":
			return []model.Obj{parent}, nil
		case "current-parent":
			return []model.Obj{file}, nil
		default:
			t.Fatal("stale/native parent identity lost")
			return nil, nil
		}
	}
	started, release, ended := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(ended)
		listG.Do(Key(d, "/"), func() ([]model.Obj, error) { close(started); <-release; return nil, nil })
	}()
	<-started
	defer func() { close(release); <-ended }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	object, err := GetFreshReadOnly(ctx, d, "/dir/电影 #%25.mkv")
	if err != nil || object != file || calls != 2 {
		t.Fatalf("fresh identity/read failed: %v, calls %d", err, calls)
	}
	if cached, _ := Cache.dirCache.Get(Key(d, "/")); cached.objs[0] != oldParent {
		t.Fatal("strict read changed the legacy browsing cache")
	}
}

func TestFreshReadEmptyFailureAndCancellationCannotBeConfused(t *testing.T) {
	d := freshFixture()
	root := &model.Object{ID: "native-root", Name: "/", IsFolder: true}
	d.list = func(context.Context, model.Obj, model.ListArgs) ([]model.Obj, error) { return nil, nil }
	if files, err := ListFreshReadOnly(context.Background(), d, root); err != nil || len(files) != 0 {
		t.Fatal("successful empty folder rejected")
	}
	pagingError := errors.New("page 2 unavailable")
	d.list = func(context.Context, model.Obj, model.ListArgs) ([]model.Obj, error) {
		return []model.Obj{nil}, pagingError
	}
	if files, err := ListFreshReadOnly(context.Background(), d, root); !errors.Is(err, pagingError) || files != nil {
		t.Fatal("paging failure became an empty snapshot")
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.list = func(context.Context, model.Obj, model.ListArgs) ([]model.Obj, error) {
		cancel()
		return []model.Obj{&model.Object{Name: "late.mkv"}}, nil
	}
	if files, err := ListFreshReadOnly(ctx, d, root); !errors.Is(err, context.Canceled) || files != nil {
		t.Fatal("canceled driver result accepted")
	}
}

func TestFreshReadRejectsAmbiguousNamesAndMalformedObjects(t *testing.T) {
	d := freshFixture()
	root := &model.Object{IsFolder: true}
	for _, files := range [][]model.Obj{
		{nil}, {(*model.Object)(nil)}, {&model.Object{Name: "../escape"}},
		{&model.Object{Name: "same.mkv"}, &model.Object{Name: "same.mkv"}},
	} {
		d.list = func(context.Context, model.Obj, model.ListArgs) ([]model.Obj, error) { return files, nil }
		if got, err := ListFreshReadOnly(context.Background(), d, root); !errors.Is(err, ErrReadOnlyInvalidResult) || got != nil {
			t.Fatal("invalid folder became complete")
		}
	}
	for _, path := range []string{"../escape", "/dir/../escape", "/dir//file", "/dir\\file", "/nul\x00file"} {
		if _, err := GetFreshReadOnly(context.Background(), d, path); err == nil {
			t.Fatalf("invalid relative path accepted: %q", path)
		}
	}
}
