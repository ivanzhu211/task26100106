package localcomm

// pairKey 用无序的成员对标识一段双边协商关系。
type pairKey struct {
	a, b int
}

func newPairKey(x, y int) pairKey {
	if x < y {
		return pairKey{a: x, b: y}
	}
	return pairKey{a: y, b: x}
}

// side 表示协商中的一个发送方向。
type side int

const (
	sideInitiator side = iota // 发起方，等待 offer 转交
	sideResponder             // 接收方，等待 answer 转交
)

// dirState 跟踪一个发送方向上候选与结束标记的转交进度。
type dirState struct {
	next    int            // 下一个待转交候选编号，从 1 开始
	buf     map[int]string // 已到达但尚未转交的候选，编号到载荷
	endN    int            // 已收到的结束标记候选总数
	hasEnd  bool           // 是否已收到结束标记
	endDone bool           // 结束标记是否已转交
}

func newDirState() *dirState {
	return &dirState{next: 1, buf: make(map[int]string)}
}

// negState 是一轮协商的状态。
type negState struct {
	id              NegotiationID
	offerForwarded  bool
	answerForwarded bool
	dirs            [2]*dirState
}

func newNegState(id NegotiationID) *negState {
	return &negState{
		id:   id,
		dirs: [2]*dirState{newDirState(), newDirState()},
	}
}

// descForwarded 报告某方向发送方对应的描述是否已转交：
// 发起方等待 offer，接收方等待 answer。
func (n *negState) descForwarded(s side) bool {
	if s == sideInitiator {
		return n.offerForwarded
	}
	return n.answerForwarded
}

// bufferCandidate 暂存候选；已转交或已暂存的重复候选直接忽略。
func (n *negState) bufferCandidate(s side, seq int, payload string) {
	d := n.dirs[s]
	if seq < d.next {
		return
	}
	if _, ok := d.buf[seq]; ok {
		return
	}
	d.buf[seq] = payload
}

// setEnd 记录结束标记；重复标记直接忽略。
func (n *negState) setEnd(s side, count int) {
	d := n.dirs[s]
	if d.hasEnd {
		return
	}
	d.hasEnd = true
	d.endN = count
}

// flush 在描述已转交的前提下，按编号顺序转交该方向候选，
// 并在全部 N 个候选转交后输出结束标记（恰好一次）。
// other 是发送方之外的另一名成员。
func (n *negState) flush(s side, other int) []Event {
	if !n.descForwarded(s) {
		return nil
	}
	d := n.dirs[s]
	from := n.id.Initiator
	if s == sideResponder {
		from = other
	}
	to := other
	if s == sideResponder {
		to = n.id.Initiator
	}
	var out []Event
	for {
		payload, ok := d.buf[d.next]
		if !ok {
			break
		}
		delete(d.buf, d.next)
		out = append(out, Event{
			Kind: EventCandidate,
			To:   to,
			Signal: Signal{
				Kind:    KindCandidate,
				From:    from,
				To:      to,
				Neg:     n.id,
				Seq:     d.next,
				Payload: payload,
			},
		})
		d.next++
	}
	if d.hasEnd && !d.endDone && d.endN == d.next-1 {
		d.endDone = true
		out = append(out, Event{
			Kind: EventCandidatesEnd,
			To:   to,
			Signal: Signal{
				Kind:  KindCandidatesEnd,
				From:  from,
				To:    to,
				Neg:   n.id,
				Count: d.endN,
			},
		})
	}
	return out
}

// pairState 保存一对成员的全部协商状态：
// 至多一个已生效方案、一个待协商方案，以及已失效协商集合
// 和 offer 未到时提前到达的候选暂存。
type pairState struct {
	active  *negState
	pending *negState
	dead    map[NegotiationID]struct{}
	orphans map[NegotiationID]*negState
}

func newPairState() *pairState {
	return &pairState{
		dead:    make(map[NegotiationID]struct{}),
		orphans: make(map[NegotiationID]*negState),
	}
}

// otherOf 返回成员对中除 m 之外的另一名成员。
func (k pairKey) otherOf(m int) int {
	if k.a == m {
		return k.b
	}
	return k.a
}

// lookup 按协商标识找到仍有效的协商；已失效的返回 nil。
func (p *pairState) lookup(id NegotiationID) *negState {
	if _, ok := p.dead[id]; ok {
		return nil
	}
	if p.active != nil && p.active.id == id {
		return p.active
	}
	if p.pending != nil && p.pending.id == id {
		return p.pending
	}
	return nil
}

// kill 使协商永久失效，丢弃其尚未转交的内容。
func (p *pairState) kill(n *negState) {
	p.dead[n.id] = struct{}{}
	delete(p.orphans, n.id)
	if p.active == n {
		p.active = nil
	}
	if p.pending == n {
		p.pending = nil
	}
}
