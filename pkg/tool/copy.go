package tool

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/jinzhu/copier"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/pkg/constant"
)

// CopyOption 定义复制选项的函数类型
type CopyOption func(*copier.Option)

// CopyWithIgnoreEmpty 设置是否忽略空值
func CopyWithIgnoreEmpty(ignoreEmpty bool) CopyOption {
	return func(o *copier.Option) {
		o.IgnoreEmpty = ignoreEmpty
	}
}

func DeepCopy[T, K any](destStruct T, srcStruct K, opts ...CopyOption) T {
	var dst = destStruct
	var src = srcStruct

	option := copier.Option{
		DeepCopy:    true,
		IgnoreEmpty: true,
		Converters: []copier.TypeConverter{
			{
				SrcType: time.Time{},
				DstType: constant.Int64,
				Fn: func(src any) (any, error) {
					s, ok := src.(time.Time)
					if !ok {
						return nil, errors.New("src type not matching")
					}
					return s.UnixMilli(), nil
				},
			},
		},
	}

	for _, opt := range opts {
		opt(&option)
	}

	_ = copier.CopyWithOption(dst, src, option)
	return dst
}

func ShallowCopy[T, K interface{}](destStruct T, srcStruct K, opts ...CopyOption) T {
	var dst = destStruct
	var src = srcStruct

	option := copier.Option{
		IgnoreEmpty: true,
		Converters: []copier.TypeConverter{
			{
				SrcType: time.Time{},
				DstType: constant.Int64,
				Fn: func(src interface{}) (interface{}, error) {
					s, ok := src.(time.Time)
					if !ok {
						return nil, errors.New("src type not matching")
					}
					return s.UnixMilli(), nil
				},
			},
		},
	}

	for _, opt := range opts {
		opt(&option)
	}

	_ = copier.CopyWithOption(dst, src, option)
	return dst
}

func Int64ToStringSlice(intSlice []int64) []string {
	strSlice := make([]string, len(intSlice))
	for i, n := range intSlice {
		strSlice[i] = strconv.FormatInt(n, 10)
	}
	return strSlice
}

func CloneMapToStruct(input any, output interface{}) error {
	// 确保 output 是一个指针，并且指向一个结构体
	val := reflect.ValueOf(output)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("output must be a pointer to a struct")
	}

	// 使用 JSON 编解码将 map 转换为结构体
	data, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("failed to marshal input: %w", err)
	}

	err = json.Unmarshal(data, output)
	if err != nil {
		return fmt.Errorf("failed to unmarshal data to struct: %w", err)
	}
	return nil
}
