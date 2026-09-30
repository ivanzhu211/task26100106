package localcomm

// Coordinator 保存一个房间的信令协调状态，全部位于内存中。
// 房间成员在创建时给定，运行期间不变；每对成员独立协商，
// 同一成员参与多对协商时互不影响。
type Coordinator struct {
	members map[int]struct{}
	pairs   map[pairKey]*pairState
}

// New 以给定的成员用户编号创建协调器。
func New(members []int) *Coordinator {
	c := &Coordinator{
		members: make(map[int]struct{}, len(members)),
		pairs:   make(map[pairKey]*pairState),
	}
	for _, m := range members {
		c.members[m] = struct{}{}
	}
	return c
}

// Submit 提交一条信令，返回本次需要按顺序转交的事件。
// 重复提交同一条信令不会产生第二份相同输出。
func (c *Coordinator) Submit(s Signal) []Event {
	if _, ok := c.members[s.From]; !ok {
		return nil
	}
	if _, ok := c.members[s.To]; !ok {
		return nil
	}
	if s.From == s.To {
		return nil
	}
	key := newPairKey(s.From, s.To)
	p := c.pairs[key]
	if p == nil {
		p = newPairState()
		c.pairs[key] = p
	}
	switch s.Kind {
	case KindOffer:
		return p.submitOffer(s)
	case KindAnswer:
		return p.submitAnswer(s)
	case KindCandidate:
		return p.submitCandidate(key, s)
	case KindCandidatesEnd:
		return p.submitEnd(key, s)
	}
	return nil
}

// submitOffer 处理 offer：建立待协商方案、解决碰撞并转交获胜 offer。
func (p *pairState) submitOffer(s Signal) []Event {
	id := s.Neg
	if id.Initiator != s.From {
		return nil
	}
	if _, ok := p.dead[id]; ok {
		return nil // 已失效协商的重放
	}
	if p.active != nil && p.active.id == id {
		return nil // 已生效方案的重复提交
	}
	if p.pending != nil && p.pending.id == id {
		return nil // 待协商方案的重复提交
	}
	if p.pending != nil {
		if p.pending.id.Initiator == id.Initiator {
			return nil // 同一发起人的过期 offer（调用方保证新 offer 编号递增）
		}
		return p.resolveCollision(s)
	}
	return p.adoptOffer(s)
}

// adoptOffer 把新 offer 立为待协商方案并转交，
// 同时接管此前提前到达、归属该协商的候选与结束标记。
func (p *pairState) adoptOffer(s Signal) []Event {
	neg := p.takeOrphan(s.Neg)
	neg.offerForwarded = true
	p.pending = neg
	out := []Event{{
		Kind: EventOffer,
		To:   s.To,
		Signal: Signal{
			Kind:    KindOffer,
			From:    s.From,
			To:      s.To,
			Neg:     s.Neg,
			Payload: s.Payload,
		},
	}}
	out = append(out, neg.flush(sideInitiator, s.To)...)
	return out
}

// resolveCollision 处理双方 offer 碰撞：发起人用户编号较小的一方获胜，
// 与到达顺序无关。被淘汰方案向双方各产生一次作废通知，
// 且作废通知排在获胜 offer 的转交之前。
func (p *pairState) resolveCollision(s Signal) []Event {
	incoming := p.takeOrphan(s.Neg)
	existing := p.pending
	var loser *negState
	incomingWins := s.Neg.Initiator < existing.id.Initiator
	if incomingWins {
		loser = existing
	} else {
		loser = incoming
	}
	other := newPairKey(s.From, s.To).otherOf(loser.id.Initiator)
	out := []Event{
		{Kind: EventInvalidate, To: loser.id.Initiator, Signal: Signal{Neg: loser.id}},
		{Kind: EventInvalidate, To: other, Signal: Signal{Neg: loser.id}},
	}
	p.kill(loser)
	if incomingWins {
		incoming.offerForwarded = true
		p.pending = incoming
		out = append(out, Event{
			Kind: EventOffer,
			To:   s.To,
			Signal: Signal{
				Kind:    KindOffer,
				From:    s.From,
				To:      s.To,
				Neg:     s.Neg,
				Payload: s.Payload,
			},
		})
		out = append(out, incoming.flush(sideInitiator, s.To)...)
	}
	return out
}

// submitAnswer 处理 answer：只有针对当前待协商方案的 answer 会被转交，
// 转交后新方案生效，旧的已生效方案立即失效。
func (p *pairState) submitAnswer(s Signal) []Event {
	if p.pending == nil || p.pending.id != s.Neg {
		return nil // 迟到的或与当前待协商方案不符的 answer
	}
	if s.From == s.Neg.Initiator {
		return nil
	}
	neg := p.pending
	neg.answerForwarded = true
	p.pending = nil
	if p.active != nil {
		p.kill(p.active) // 旧方案被替换，未转交内容全部丢弃
	}
	p.active = neg
	out := []Event{{
		Kind: EventAnswer,
		To:   s.To,
		Signal: Signal{
			Kind:    KindAnswer,
			From:    s.From,
			To:      s.To,
			Neg:     s.Neg,
			Payload: s.Payload,
		},
	}}
	out = append(out, neg.flush(sideResponder, s.From)...)
	return out
}

// submitCandidate 处理 ICE 候选：归属未定的候选先暂存，
// 归属已失效协商的候选直接丢弃。
func (p *pairState) submitCandidate(key pairKey, s Signal) []Event {
	sd := sideOf(s)
	n := p.lookup(s.Neg)
	if n == nil {
		if _, ok := p.dead[s.Neg]; ok {
			return nil
		}
		n = p.takeOrphan(s.Neg)
		p.orphans[s.Neg] = n
	}
	n.bufferCandidate(sd, s.Seq, s.Payload)
	return n.flush(sd, key.otherOf(s.From))
}

// submitEnd 处理候选结束标记：只有对应描述与全部 N 个候选
// 转交后才输出，且恰好一次；先到不丢弃后续候选。
func (p *pairState) submitEnd(key pairKey, s Signal) []Event {
	sd := sideOf(s)
	n := p.lookup(s.Neg)
	if n == nil {
		if _, ok := p.dead[s.Neg]; ok {
			return nil
		}
		n = p.takeOrphan(s.Neg)
		p.orphans[s.Neg] = n
	}
	n.setEnd(sd, s.Count)
	return n.flush(sd, key.otherOf(s.From))
}

// sideOf 按发送人判断候选/结束标记所属的发送方向。
func sideOf(s Signal) side {
	if s.From == s.Neg.Initiator {
		return sideInitiator
	}
	return sideResponder
}

// takeOrphan 取出归属某协商的暂存状态，没有则新建。
func (p *pairState) takeOrphan(id NegotiationID) *negState {
	if n, ok := p.orphans[id]; ok {
		delete(p.orphans, id)
		return n
	}
	return newNegState(id)
}
