package op

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

var ErrReadOnlyInvalidResult = errors.New("目录结果包含无效或有歧义的条目，不能作为完整快照")

func nilReadObject(obj model.Obj) bool {
	if obj == nil {
		return true
	}
	value := reflect.ValueOf(obj)
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Interface, reflect.Slice, reflect.Func, reflect.Chan:
		return value.IsNil()
	}
	return false
}

func validSourceName(name string) bool {
	return name != "" && name != "." && name != ".." && utf8.ValidString(name) && !strings.ContainsAny(name, "/\\") && !strings.ContainsFunc(name, unicode.IsControl)
}

func readOnlyStatus(ctx context.Context, storage driver.Driver) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if storage == nil || storage.GetStorage() == nil || storage.GetStorage().Disabled || storage.GetStorage().Status != WORK {
		return errs.StorageNotInit
	}
	return nil
}

// ListFreshReadOnly 接受已解析的原生父对象；完整快照不依赖浏览缓存或普通请求的singleflight参数。作者：long
func ListFreshReadOnly(ctx context.Context, storage driver.Driver, parent model.Obj) (objects []model.Obj, err error) {
	if err = readOnlyStatus(ctx, storage); err != nil {
		return nil, err
	}
	if nilReadObject(parent) {
		return nil, ErrReadOnlyInvalidResult
	}
	parent = model.UnwrapObjName(parent)
	if nilReadObject(parent) || !parent.IsDir() {
		return nil, errs.NotFolder
	}
	// long: 不加入旧flight、不读写旧目录缓存、不调用旧Hook；强制刷新也传给驱动自身，防止缓存伪造完整扫描。
	objects, err = storage.List(ctx, parent, model.ListArgs{Refresh: true, SkipHook: true})
	if err != nil {
		return nil, fmt.Errorf("目录读取失败: %w", err)
	}
	if err = readOnlyStatus(ctx, storage); err != nil {
		// long: 老驱动即使在取消后返回成功，也不能提交这份结果为完整快照。
		return nil, err
	}
	for _, object := range objects {
		if nilReadObject(object) || !validSourceName(object.GetName()) {
			return nil, ErrReadOnlyInvalidResult
		}
	}
	// long: 名称包装不能修改驱动共享的切片；保留底层对象供后续按ID取链，而不是手工重建文件对象。
	objects = append([]model.Obj(nil), objects...)
	wrapObjsName(storage, objects)
	names := make(map[string]bool, len(objects))
	for _, object := range objects {
		name := object.GetName()
		if !validSourceName(name) || names[name] {
			return nil, ErrReadOnlyInvalidResult
		}
		names[name] = true
	}
	return objects, nil
}

// GetFreshReadOnly 从配置根开始逐层重新列举，避免Refresh列表仍借用已失效的祖先对象缓存。作者：long
func GetFreshReadOnly(ctx context.Context, storage driver.Driver, relativePath string) (model.Obj, error) {
	if err := readOnlyStatus(ctx, storage); err != nil {
		return nil, err
	}
	if relativePath == "" {
		relativePath = "/"
	}
	if !strings.HasPrefix(relativePath, "/") {
		return nil, fmt.Errorf("源路径必须从当前存储根目录开始")
	}
	var parts []string
	if relativePath != "/" {
		parts = strings.Split(strings.TrimSuffix(relativePath, "/")[1:], "/")
		for _, part := range parts {
			if !validSourceName(part) {
				return nil, fmt.Errorf("源路径含无效层级")
			}
		}
	}
	// long: op.Get只有根目录分支不走祖先缓存；保持GetRooter/RootID/RootPath的原有原生根对象契约。
	object, err := GetUnwrap(ctx, storage, "/")
	if err != nil {
		return nil, err
	}
	if nilReadObject(object) || !object.IsDir() {
		return nil, ErrReadOnlyInvalidResult
	}
	for _, part := range parts {
		children, err := ListFreshReadOnly(ctx, storage, object)
		if err != nil {
			return nil, err
		}
		object = nil
		for _, child := range children {
			if model.UnwrapObjName(child).GetName() == part {
				object = model.UnwrapObjName(child)
				break
			}
		}
		if object == nil {
			return nil, errs.ObjectNotFound
		}
	}
	if err = readOnlyStatus(ctx, storage); err != nil {
		return nil, err
	}
	return object, nil
}
