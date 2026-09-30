package localcomm

// Coordinator 是信令协调器，状态全部保存在内存中。
// 房间成员在创建时给定，运行期间不变；每对成员独立协商，
// 同一成员参与多对协商时互不影响。
type Coordinator struct {
	members map[int]struct{}
	pairs   map[pairKey]*pairState
}

// NewCoordinator 以给定的成员用户编号创建协调器。
func NewCoordinator(members []int) *Coordinator {
	c := &Coordinator{
		members: map[int]struct{}{},
		pairs:   map[pairKey]*pairState{},
	}
	for _, m := range members {
		c.members[m] = struct{}{}
	}
	return c
}

// Submit 提交一条信令，返回本次需要按顺序转交的事件。
// 重复提交同一条信令不会产生第二份相同输出。
func (c *Coordinator) Submit(s Signal) []Event {
	if s.From == s.To {
		return nil
	}
	if _, ok := c.members[s.From]; !ok {
		return nil
	}
	if _, ok := c.members[s.To]; !ok {
		return nil
	}
	key := makePairKey(s.From, s.To)
	p, ok := c.pairs[key]
	if !ok {
		p = newPairState(key)
		c.pairs[key] = p
	}
	return p.submit(s)
}
