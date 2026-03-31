package hydrationfn

import "context"

type Hydration struct{}

func (s *Hydration) Run(ctx context.Context) error {
	// todo 查询数据库的表，提取 未渲染的 app，去访问 chart-repo 接口，拿到渲染结构后更新数据库；渲染失败标记失败原因
	return nil
}
