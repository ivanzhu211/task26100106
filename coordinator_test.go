package localcomm

import (
	"reflect"
	"testing"
)

func offer(from, to, round int, payload string) Signal {
	return Signal{Kind: KindOffer, From: from, To: to, Neg: NegotiationID{Initiator: from, Round: round}, Payload: payload}
}

func answer(from, to, initiator, round int, payload string) Signal {
	return Signal{Kind: KindAnswer, From: from, To: to, Neg: NegotiationID{Initiator: initiator, Round: round}, Payload: payload}
}

func candidate(from, to, initiator, round, seq int, payload string) Signal {
	return Signal{Kind: KindCandidate, From: from, To: to, Neg: NegotiationID{Initiator: initiator, Round: round}, Seq: seq, Payload: payload}
}

func end(from, to, initiator, round, count int) Signal {
	return Signal{Kind: KindCandidatesEnd, From: from, To: to, Neg: NegotiationID{Initiator: initiator, Round: round}, Count: count}
}

func summarize(evs []Event) []string {
	var out []string
	for _, e := range evs {
		s := ""
		switch e.Kind {
		case EventOffer:
			s = "offer"
		case EventAnswer:
			s = "answer"
		case EventCandidate:
			s = "candidate"
		case EventCandidatesEnd:
			s = "end"
		case EventInvalidate:
			s = "invalidate"
		}
		s += " to=" + itoa(e.To)
		s += " neg=(" + itoa(e.Signal.Neg.Initiator) + "," + itoa(e.Signal.Neg.Round) + ")"
		if e.Kind == EventCandidate {
			s += " seq=" + itoa(e.Signal.Seq) + " payload=" + e.Signal.Payload
		}
		if e.Kind == EventCandidatesEnd {
			s += " count=" + itoa(e.Signal.Count)
		}
		if e.Kind == EventOffer || e.Kind == EventAnswer {
			s += " payload=" + e.Signal.Payload
		}
		out = append(out, s)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func expect(t *testing.T, step string, got []Event, want []string) {
	t.Helper()
	gs := summarize(got)
	if gs == nil {
		gs = []string{}
	}
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(gs, want) {
		t.Fatalf("%s:\n got=%q\nwant=%q", step, gs, want)
	}
}

// 场景一：大编号成员的 offer 及部分候选已转交后，小编号成员的 offer 到达，
// 再收到被淘汰方案的 answer。只有获胜方案能生效；
// 旧的已生效方案在新 answer 前仍可传递候选。
func TestGlareSmallerInitiatorWins(t *testing.T) {
	c := New([]int{1, 2, 3})

	// 建立已生效方案 {2,1}。
	expect(t, "active offer", c.Submit(offer(2, 1, 1, "sdp-a")),
		[]string{"offer to=1 neg=(2,1) payload=sdp-a"})
	expect(t, "active answer", c.Submit(answer(1, 2, 2, 1, "sdp-b")),
		[]string{"answer to=2 neg=(2,1) payload=sdp-b"})

	// 大编号成员 2 发起新一轮 {2,2}，offer 与部分候选转交。
	expect(t, "pending offer", c.Submit(offer(2, 1, 2, "sdp-c")),
		[]string{"offer to=1 neg=(2,2) payload=sdp-c"})
	expect(t, "pending candidates", c.Submit(candidate(2, 1, 2, 2, 1, "c1")),
		[]string{"candidate to=1 neg=(2,2) seq=1 payload=c1"})
	expect(t, "pending candidates 2", c.Submit(candidate(2, 1, 2, 2, 2, "c2")),
		[]string{"candidate to=1 neg=(2,2) seq=2 payload=c2"})

	// 小编号成员 1 的 offer {1,1} 到达，碰撞：1 获胜。
	// 作废通知双方各一条，且排在获胜 offer 之前。
	expect(t, "glare offer", c.Submit(offer(1, 2, 1, "sdp-d")),
		[]string{
			"invalidate to=2 neg=(2,2)",
			"invalidate to=1 neg=(2,2)",
			"offer to=2 neg=(1,1) payload=sdp-d",
		})

	// 被淘汰方案的迟到 answer 与候选、重放 offer 全部失效。
	expect(t, "loser answer", c.Submit(answer(1, 2, 2, 2, "sdp-e")), nil)
	expect(t, "loser candidate", c.Submit(candidate(2, 1, 2, 2, 3, "c3")), nil)
	expect(t, "loser offer replay", c.Submit(offer(2, 1, 2, "sdp-c")), nil)

	// 新 answer 到达前，旧的已生效方案 {2,1} 仍可传递候选。
	expect(t, "old active candidate", c.Submit(candidate(2, 1, 2, 1, 1, "old1")),
		[]string{"candidate to=1 neg=(2,1) seq=1 payload=old1"})

	// 获胜方案接收方候选先到，等 answer 转交后放行。
	expect(t, "winner responder early", c.Submit(candidate(2, 1, 1, 1, 1, "r1")), nil)

	// 获胜 answer 到达：转交并生效，旧方案 {2,1} 自此失效。
	expect(t, "winner answer", c.Submit(answer(2, 1, 1, 1, "sdp-f")),
		[]string{
			"answer to=1 neg=(1,1) payload=sdp-f",
			"candidate to=1 neg=(1,1) seq=1 payload=r1",
		})

	// 旧方案信令全部失效。
	expect(t, "old active dead", c.Submit(candidate(2, 1, 2, 1, 2, "old2")), nil)
	expect(t, "winner offer replay", c.Submit(offer(1, 2, 1, "sdp-d")), nil)
	expect(t, "winner answer replay", c.Submit(answer(2, 1, 1, 1, "sdp-f")), nil)

	// 同一成员参与多对协商互不影响：{1,3} 独立协商。
	expect(t, "other pair offer", c.Submit(offer(3, 1, 1, "sdp-g")),
		[]string{"offer to=1 neg=(3,1) payload=sdp-g"})
	expect(t, "other pair candidate", c.Submit(candidate(3, 1, 3, 1, 1, "x1")),
		[]string{"candidate to=1 neg=(3,1) seq=1 payload=x1"})
}

// 场景二：候选与结束标记先到，且有缺号和重复。
// 补齐后各方向输出须与完整编号序列逐项一致，描述在前、结束标记在后。
func TestEarlyOutOfOrderCandidatesAndEnd(t *testing.T) {
	c := New([]int{10, 20})

	// 发起方方向的候选与结束标记全部先于 offer 到达，乱序、缺号、带重复。
	expect(t, "early c2", c.Submit(candidate(10, 20, 10, 1, 2, "i2")), nil)
	expect(t, "early end", c.Submit(end(10, 20, 10, 1, 3)), nil)
	expect(t, "early c1", c.Submit(candidate(10, 20, 10, 1, 1, "i1")), nil)
	expect(t, "dup c2", c.Submit(candidate(10, 20, 10, 1, 2, "i2")), nil)
	expect(t, "dup end", c.Submit(end(10, 20, 10, 1, 3)), nil)

	// 接收方方向同样先到。
	expect(t, "early rc2", c.Submit(candidate(20, 10, 10, 1, 2, "r2")), nil)
	expect(t, "early rend", c.Submit(end(20, 10, 10, 1, 2)), nil)
	expect(t, "early rc1", c.Submit(candidate(20, 10, 10, 1, 1, "r1")), nil)

	// offer 到达：描述在前，发起方候选按 1,2 顺序（3 号缺号不能越过），结束标记等待。
	expect(t, "offer", c.Submit(offer(10, 20, 1, "sdp-o")),
		[]string{
			"offer to=20 neg=(10,1) payload=sdp-o",
			"candidate to=20 neg=(10,1) seq=1 payload=i1",
			"candidate to=20 neg=(10,1) seq=2 payload=i2",
		})

	// 结束标记先到不丢弃后续候选：补齐 3 号后，候选与结束标记依次输出。
	expect(t, "fill gap", c.Submit(candidate(10, 20, 10, 1, 3, "i3")),
		[]string{
			"candidate to=20 neg=(10,1) seq=3 payload=i3",
			"end to=20 neg=(10,1) count=3",
		})

	// answer 到达：接收方候选按序转交，结束标记殿后。
	expect(t, "answer", c.Submit(answer(20, 10, 10, 1, "sdp-a")),
		[]string{
			"answer to=10 neg=(10,1) payload=sdp-a",
			"candidate to=10 neg=(10,1) seq=1 payload=r1",
			"candidate to=10 neg=(10,1) seq=2 payload=r2",
			"end to=10 neg=(10,1) count=2",
		})

	// 重复提交不再产生输出。
	expect(t, "dup offer", c.Submit(offer(10, 20, 1, "sdp-o")), nil)
	expect(t, "dup answer", c.Submit(answer(20, 10, 10, 1, "sdp-a")), nil)
	expect(t, "dup c1", c.Submit(candidate(10, 20, 10, 1, 1, "i1")), nil)
	expect(t, "dup end i", c.Submit(end(10, 20, 10, 1, 3)), nil)
	expect(t, "dup end r", c.Submit(end(20, 10, 10, 1, 2)), nil)
}

// 零候选方向：结束标记 N=0 在描述转交后立即输出。
func TestZeroCandidateEnd(t *testing.T) {
	c := New([]int{5, 6})
	expect(t, "end first", c.Submit(end(5, 6, 5, 1, 0)), nil)
	expect(t, "offer", c.Submit(offer(5, 6, 1, "o")),
		[]string{
			"offer to=6 neg=(5,1) payload=o",
			"end to=6 neg=(5,1) count=0",
		})
	expect(t, "answer", c.Submit(answer(6, 5, 5, 1, "a")),
		[]string{"answer to=5 neg=(5,1) payload=a"})
	expect(t, "responder end", c.Submit(end(6, 5, 5, 1, 0)),
		[]string{"end to=5 neg=(5,1) count=0"})
}

// 碰撞中后到的较大编号 offer 被淘汰：只产生作废通知，
// 原待协商方案与已生效方案都不受影响。
func TestGlareIncomingLargerLoses(t *testing.T) {
	c := New([]int{1, 2})

	// 已生效方案 {2,1}。
	c.Submit(offer(2, 1, 1, "a"))
	c.Submit(answer(1, 2, 2, 1, "b"))

	// 小编号成员 1 先提出 {1,1}。
	expect(t, "small offer first", c.Submit(offer(1, 2, 1, "c")),
		[]string{"offer to=2 neg=(1,1) payload=c"})

	// 大编号成员 2 的 {2,2} 后到达，被淘汰：双方各一条作废通知，无 offer 转交。
	expect(t, "large offer loses", c.Submit(offer(2, 1, 2, "d")),
		[]string{
			"invalidate to=2 neg=(2,2)",
			"invalidate to=1 neg=(2,2)",
		})

	// 已生效方案 {2,1} 未被碰撞破坏，仍可传候选。
	expect(t, "active intact", c.Submit(candidate(2, 1, 2, 1, 1, "k1")),
		[]string{"candidate to=1 neg=(2,1) seq=1 payload=k1"})

	// 原待协商方案 {1,1} 继续推进直至生效。
	expect(t, "pending survives", c.Submit(answer(2, 1, 1, 1, "e")),
		[]string{"answer to=1 neg=(1,1) payload=e"})

	// 被淘汰方案永久失效。
	expect(t, "loser dead", c.Submit(candidate(2, 1, 2, 2, 1, "k2")), nil)
	expect(t, "loser answer dead", c.Submit(answer(1, 2, 2, 2, "f")), nil)
}
