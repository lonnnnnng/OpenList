package strm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type sourceFixtureDriver struct {
	model.Storage
	driver.RootID
	list func(context.Context, model.Obj, model.ListArgs) ([]model.Obj, error)
}

func (d *sourceFixtureDriver) Config() driver.Config          { return driver.Config{Name: d.Driver} }
func (d *sourceFixtureDriver) GetAddition() driver.Additional { return &d.RootID }
func (d *sourceFixtureDriver) Init(context.Context) error     { return nil }
func (d *sourceFixtureDriver) Drop(context.Context) error     { return nil }
func (d *sourceFixtureDriver) List(ctx context.Context, parent model.Obj, args model.ListArgs) ([]model.Obj, error) {
	return d.list(ctx, parent, args)
}
func (d *sourceFixtureDriver) Link(context.Context, model.Obj, model.LinkArgs) (*model.Link, error) {
	return nil, errors.New("unused")
}

func sourceFixture(t *testing.T) (sourceSnapshot, func(context.Context, uint) (sourceSnapshot, error), func(sourceSnapshot)) {
	t.Helper()
	revision := time.Date(2026, 11, 17, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	parent := &model.Object{ID: "movies-id", Name: "Movies", IsFolder: true}
	storage := model.Storage{ID: 22, MountPath: "/cloud", Driver: "Quark", Status: op.WORK, Modified: revision, Addition: `{"root_folder_id":"0","cookie":"original-cookie"}`}
	d := &sourceFixtureDriver{Storage: storage, RootID: driver.RootID{RootFolderID: "0"}}
	d.list = func(_ context.Context, object model.Obj, args model.ListArgs) ([]model.Obj, error) {
		if !args.Refresh || !args.SkipHook {
			t.Fatal("source listing must bypass cache and hooks")
		}
		if object.GetID() == "0" {
			return []model.Obj{parent}, nil
		}
		return []model.Obj{&model.Object{ID: "movie-1", Name: "film.mkv", Size: 42, Modified: revision}}, nil
	}
	snapshot := &sourceSnapshot{record: &storage, driver: d, mounts: []model.Storage{storage}}
	lookup := func(ctx context.Context, id uint) (sourceSnapshot, error) {
		if err := ctx.Err(); err != nil {
			return sourceSnapshot{}, err
		}
		if id != storage.ID {
			return sourceSnapshot{}, errors.New("storage not found")
		}
		return *snapshot, nil
	}
	return *snapshot, lookup, func(next sourceSnapshot) { *snapshot = next }
}

func TestSourceBindsByStorageIDAndKeepsNativeObjects(t *testing.T) {
	snapshot, lookup, update := sourceFixture(t)
	source, err := openSource(context.Background(), 22, "/Movies", lookup)
	if err != nil {
		t.Fatal(err)
	}
	if source.Root().RemoteID != "movies-id" || source.Root().Path != "/Movies" {
		t.Fatalf("wrong selected source: %+v", source.Root())
	}
	children, err := source.List(context.Background(), source.Root())
	if err != nil || len(children) != 1 || children[0].RemoteID != "movie-1" || children[0].Path != "/Movies/film.mkv" {
		t.Fatalf("native child lost: %+v, %v", children, err)
	}

	// long: 刷新令牌轮换不代表账号根变化；Cookie不进入指纹，状态和路径仍由存储ID绑定。
	updated := *snapshot.record
	updated.Addition = `{"root_folder_id":"0","cookie":"rotated-cookie"}`
	snapshot.record = &updated
	update(snapshot)
	if err = source.check(context.Background()); err != nil {
		t.Fatalf("credential rotation invalidated source: %v", err)
	}

	manualEdit := updated
	manualEdit.Modified = manualEdit.Modified.Add(time.Second)
	snapshot.record = &manualEdit
	update(snapshot)
	if err = source.check(context.Background()); err == nil {
		t.Fatal("manual storage edit kept old source binding")
	}
}

func TestSourceRejectsChangedStorageAndOverlappingTopology(t *testing.T) {
	snapshot, _, update := sourceFixture(t)
	root := *snapshot.record
	submount := model.Storage{ID: 23, MountPath: "/cloud/Movies/sub", Driver: "Local"}
	snapshot.mounts = append(snapshot.mounts, submount)
	update(snapshot)
	if _, err := sourceFingerprint(snapshot, "/Movies", root.ID); err == nil {
		t.Fatal("overlapping child storage accepted")
	}

	snapshot.mounts = []model.Storage{root}
	disabled := root
	disabled.Disabled = true
	snapshot.record = &disabled
	update(snapshot)
	if _, err := sourceFingerprint(snapshot, "/Movies", root.ID); err == nil {
		t.Fatal("disabled storage accepted")
	}

	snapshot.record = &root
	changedMount := root
	changedMount.MountPath = "/replacement"
	snapshot.record = &changedMount
	update(snapshot)
	if _, err := sourceFingerprint(snapshot, "/Movies", root.ID); err == nil {
		t.Fatal("stale runtime mount accepted")
	}

}

func TestSourceRejectsIncompleteVisibilityAndCanceledRead(t *testing.T) {
	snapshot, lookup, update := sourceFixture(t)
	filtered := *snapshot.record
	filtered.Addition = `{"root_folder_id":"0","only_list_video_file":true}`
	snapshot.record = &filtered
	update(snapshot)
	if _, err := sourceFingerprint(snapshot, "/", filtered.ID); err == nil {
		t.Fatal("video-filtered source accepted")
	}

	if err := validateSourceVisibility("Local", false, false); err == nil {
		t.Fatal("hidden files omitted by Local accepted")
	}

	// long: 一个驱动即使在context取消后仍返回成功，也不能让不完整读取成为缺失候选。
	_, lookup, _ = sourceFixture(t)
	source, err := openSource(context.Background(), 22, "/", lookup)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	source.driver.(*sourceFixtureDriver).list = func(context.Context, model.Obj, model.ListArgs) ([]model.Obj, error) {
		cancel()
		return []model.Obj{}, nil
	}
	if files, err := source.List(ctx, source.Root()); files != nil || err == nil {
		t.Fatal("canceled read was accepted as complete")
	}
}

func TestLocalSymlinkIsExplicitlyMarked(t *testing.T) {
	_, lookup, _ := sourceFixture(t)
	source, err := openSource(context.Background(), 22, "/", lookup)
	if err != nil {
		t.Fatal(err)
	}
	source.driver.(*sourceFixtureDriver).list = func(context.Context, model.Obj, model.ListArgs) ([]model.Obj, error) {
		return []model.Obj{&markedSourceSymlink{Object: model.Object{Name: "link", IsFolder: true}}}, nil
	}
	if children, err := source.List(context.Background(), source.Root()); children != nil || err == nil {
		t.Fatal("Local symlink was accepted")
	}
}

func TestDraftValidationDoesNotOpenCloudForInvalidSettings(t *testing.T) {
	valid := TaskDraft{Name: "  电影  ", StorageID: 22, SourcePath: "/", OutputSubdir: "movies"}
	cases := []TaskDraft{
		{Name: "", StorageID: 22, SourcePath: "/", OutputSubdir: "movies"},
		{Name: "x", StorageID: 22, SourcePath: "/../escape", OutputSubdir: "movies"},
		{Name: "x", StorageID: 22, SourcePath: "/", OutputSubdir: "../escape"},
		{Name: "x", StorageID: 22, SourcePath: "/", OutputSubdir: "movies", Cron: "* * * * *"},
		{Name: "x", StorageID: 22, SourcePath: "/", OutputSubdir: "movies", DeleteSync: true},
		{Name: "x", StorageID: 22, SourcePath: "/", OutputSubdir: "movies", Extensions: []string{".txt"}},
	}
	for _, draft := range cases {
		if _, _, err := canonicalDraft(context.Background(), draft); err == nil {
			t.Fatalf("invalid draft accepted: %+v", draft)
		}
	}
	if valid.Name == "" {
		t.Fatal("test fixture invalid")
	}
}

type previewFixture struct {
	root     SourceNode
	children map[string][]SourceNode
	fail     map[string]error
}

func (f previewFixture) Root() SourceNode { return f.root }
func (f previewFixture) List(_ context.Context, parent SourceNode) ([]SourceNode, error) {
	if err := f.fail[parent.Path]; err != nil {
		return nil, err
	}
	return f.children[parent.Path], nil
}

func TestPreviewMarksChildListFailurePartialAndKeepsCandidatesReadOnly(t *testing.T) {
	task := &model.STRMTask{ID: "task", State: "draft", OutputSubdir: "movies", Extensions: []string{".mkv"}}
	root := SourceNode{Path: "/", IsDir: true}
	childDir := SourceNode{Path: "/broken", IsDir: true}
	film := SourceNode{Path: "/film.mkv", OriginalName: "film.mkv", Size: 100, RemoteID: "remote-1"}
	reader := previewFixture{root: root, children: map[string][]SourceNode{"/": {childDir, film}}, fail: map[string]error{"/broken": errors.New("page 2 unavailable")}}
	result := buildTaskPreview(context.Background(), task, reader, "preview-run")
	if result.Complete || !result.ReadOnly || result.Candidates != 1 || len(result.Items) != 1 || len(result.Issues) != 1 {
		t.Fatalf("partial preview semantics lost: %+v", result)
	}
	if result.Issues[0].Path != "/broken" || result.Items[0].OutputPath != "movies/film.mkv.strm" {
		t.Fatalf("wrong preview detail: %+v", result)
	}
}

type markedSourceSymlink struct{ model.Object }

func (*markedSourceSymlink) IsSourceSymlink() bool { return true }
