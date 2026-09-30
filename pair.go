package localcomm

// pairKey 是成员对的无序键。
type pairKey struct{ lo, hi int }

func makePairKey(a, b int) pairKey {
	if a < b {
		return pairKey{a, b}
	}
	return pairKey{b, a}
}

// pairState 保存一对成员之间的协商状态：
// 至多一个已生效方案和一个待协商方案。
type pairState struct {
	key     pairKey
	active  *negState           // 已生效方案，可为空
	pending *negState           // 待协商方案，可为空
	maxSeq  map[int]int         // 每个发起人已见到的最大协商编号
	future  map[NegID]*negState // offer 未到、先收到候选/结束标记的暂存
}

func newPairState(key pairKey) *pairState {
	return &pairState{
		key:    key,
		maxSeq: map[int]int{},
		future: map[NegID]*negState{},
	}
}

// other 返回成员对中除 m 外的另一方。
func (p *pairState) other(m int) int {
	if m == p.key.lo {
		return p.key.hi
	}
	return p.key.lo
}

func (p *pairState) submit(s Signal) []Event {
	switch s.Kind {
	case KindOffer:
		return p.submitOffer(s)
	case KindAnswer:
		return p.submitAnswer(s)
	case KindCandidate:
		return p.submitCandidate(s)
	case KindEndOfCandidates:
		return p.submitEnd(s)
	}
	return nil
}

// owner 找到协商标识当前归属的状态。ok 为 false 表示该协商已永久失效。
func (p *pairState) owner(id NegID) (*negState, bool) {
	if p.active != nil && p.active.id == id {
		return p.active, true
	}
	if p.pending != nil && p.pending.id == id {
		return p.pending, true
	}
	if id.Seq > p.maxSeq[id.Initiator] {
		neg, ok := p.future[id]
		if !ok {
			neg = newNegState(id, p.other(id.Initiator))
			p.future[id] = neg
		}
		return neg, true
	}
	return nil, false
}

func (p *pairState) submitOffer(s Signal) []Event {
	id := s.Neg
	if p.active != nil && p.active.id == id {
		return nil // 重放
	}
	if p.pending != nil && p.pending.id == id {
		return nil // 重放
	}
	if id.Seq <= p.maxSeq[id.Initiator] {
		return nil // 已失效协商的重放
	}
	p.maxSeq[id.Initiator] = id.Seq

	neg, ok := p.future[id]
	if ok {
		delete(p.future, id)
	} else {
		neg = newNegState(id, p.other(id.Initiator))
	}

	if p.pending == nil {
		neg.offerSent = true
		p.pending = neg
		ev := []Event{{Kind: EventDeliver, To: s.To, Signal: s}}
		return append(ev, neg.flush(id.Initiator)...)
	}

	// 待协商期间发生 offer 碰撞：发起人用户编号较小者获胜，与到达顺序无关。
	if id.Initiator < p.pending.id.Initiator {
		loser := p.pending
		p.pending = neg
		neg.offerSent = true
		ev := invalidateEvents(loser.id, p.key)
		ev = append(ev, Event{Kind: EventDeliver, To: s.To, Signal: s})
		return append(ev, neg.flush(id.Initiator)...)
	}
	// 新 offer 被淘汰：向双方各产生一次作废通知，不转交。
	return invalidateEvents(id, p.key)
}

func (p *pairState) submitAnswer(s Signal) []Event {
	if p.pending == nil || p.pending.id != s.Neg {
		return nil // 重放或已失效
	}
	neg := p.pending
	neg.answerSent = true
	// 新方案生效，旧的已生效方案永久失效，未转交内容丢弃。
	p.active = neg
	p.pending = nil
	ev := []Event{{Kind: EventDeliver, To: s.To, Signal: s}}
	return append(ev, neg.flush(neg.receiver)...)
}

func (p *pairState) submitCandidate(s Signal) []Event {
	neg, ok := p.owner(s.Neg)
	if !ok {
		return nil
	}
	neg.addCandidate(s)
	return neg.flush(s.From)
}

func (p *pairState) submitEnd(s Signal) []Event {
	neg, ok := p.owner(s.Neg)
	if !ok {
		return nil
	}
	neg.addEnd(s)
	return neg.flush(s.From)
}

// invalidateEvents 为被淘汰的协商生成双方各一次的作废通知。
func invalidateEvents(id NegID, key pairKey) []Event {
	return []Event{
		{Kind: EventInvalidate, To: key.lo, Neg: id},
		{Kind: EventInvalidate, To: key.hi, Neg: id},
	}
}
