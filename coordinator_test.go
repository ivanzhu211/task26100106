package localcomm

import (
	"reflect"
	"testing"
)

func offer(from, to, init, seq int, payload string) Signal {
	return Signal{Kind: KindOffer, From: from, To: to, Neg: NegID{init, seq}, Payload: []byte(payload)}
}

func answer(from, to, init, seq int, payload string) Signal {
	return Signal{Kind: KindAnswer, From: from, To: to, Neg: NegID{init, seq}, Payload: []byte(payload)}
}

func cand(from, to, init, seq, candSeq int, payload string) Signal {
	return Signal{Kind: KindCandidate, From: from, To: to, Neg: NegID{init, seq}, CandSeq: candSeq, Payload: []byte(payload)}
}

func end(from, to, init, seq, total int) Signal {
	return Signal{Kind: KindEndOfCandidates, From: from, To: to, Neg: NegID{init, seq}, CandTotal: total}
}

// delivered 过滤出转交事件，返回 (接收人, 信令) 序列。
func delivered(ev []Event) []Signal {
	var out []Signal
	for _, e := range ev {
		if e.Kind == EventDeliver {
			out = append(out, e.Signal)
		}
	}
	return out
}

// 场景一：碰撞中小编号获胜，被淘汰方案的迟到 answer 无效，
// 旧的已生效方案在新 answer 前仍可传递候选。
func TestCollisionLowerInitiatorWins(t *testing.T) {
	c := NewCoordinator([]int{1, 2})

	// 建立旧的已生效方案 {1,1}。
	if ev := c.Submit(offer(1, 2, 1, 1, "o1")); len(delivered(ev)) != 1 {
		t.Fatalf("offer {1,1}: want 1 deliver, got %v", ev)
	}
	if ev := c.Submit(answer(2, 1, 1, 1, "a1")); len(delivered(ev)) != 1 {
		t.Fatalf("answer {1,1}: want 1 deliver, got %v", ev)
	}

	// 大编号成员 2 发起新协商 {2,1}，offer 与部分候选被转交。
	if ev := c.Submit(offer(2, 1, 2, 1, "o2")); len(delivered(ev)) != 1 {
		t.Fatalf("offer {2,1}: want 1 deliver, got %v", ev)
	}
	if ev := c.Submit(cand(2, 1, 2, 1, 1, "c2-1")); len(delivered(ev)) != 1 {
		t.Fatalf("cand {2,1}#1: want 1 deliver, got %v", ev)
	}
	if ev := c.Submit(cand(2, 1, 2, 1, 2, "c2-2")); len(delivered(ev)) != 1 {
		t.Fatalf("cand {2,1}#2: want 1 deliver, got %v", ev)
	}

	// 小编号成员 1 的 offer {1,2} 到达：碰撞，{1,2} 获胜。
	// 作废通知必须在获胜 offer 之前。
	ev := c.Submit(offer(1, 2, 1, 2, "o3"))
	want := []Event{
		{Kind: EventInvalidate, To: 1, Neg: NegID{2, 1}},
		{Kind: EventInvalidate, To: 2, Neg: NegID{2, 1}},
		{Kind: EventDeliver, To: 2, Signal: offer(1, 2, 1, 2, "o3")},
	}
	if !reflect.DeepEqual(ev, want) {
		t.Fatalf("collision: want %v, got %v", want, ev)
	}

	// 旧的已生效方案 {1,1} 在新 answer 前仍可双向传递候选。
	if ev := c.Submit(cand(2, 1, 1, 1, 1, "old-c2")); len(delivered(ev)) != 1 {
		t.Fatalf("old active cand from 2: want 1 deliver, got %v", ev)
	}
	if ev := c.Submit(cand(1, 2, 1, 1, 1, "old-c1")); len(delivered(ev)) != 1 {
		t.Fatalf("old active cand from 1: want 1 deliver, got %v", ev)
	}

	// 被淘汰方案的迟到 answer、候选与重放 offer 全部无效。
	if ev := c.Submit(answer(1, 2, 2, 1, "late-a")); len(ev) != 0 {
		t.Fatalf("late answer of loser: want no events, got %v", ev)
	}
	if ev := c.Submit(cand(2, 1, 2, 1, 3, "late-c")); len(ev) != 0 {
		t.Fatalf("late cand of loser: want no events, got %v", ev)
	}
	if ev := c.Submit(offer(2, 1, 2, 1, "o2")); len(ev) != 0 {
		t.Fatalf("replayed loser offer: want no events, got %v", ev)
	}

	// 获胜方案的 answer 到达：转交，{1,2} 生效。
	ev = c.Submit(answer(2, 1, 1, 2, "a3"))
	if got := delivered(ev); len(got) != 1 || got[0].Kind != KindAnswer {
		t.Fatalf("winner answer: want 1 answer deliver, got %v", ev)
	}

	// 此后旧方案 {1,1} 信令全部失效。
	if ev := c.Submit(cand(1, 2, 1, 1, 2, "old-c1b")); len(ev) != 0 {
		t.Fatalf("replaced active cand: want no events, got %v", ev)
	}
	// 新方案接收方候选正常转交。
	if ev := c.Submit(cand(2, 1, 1, 2, 1, "new-c2")); len(delivered(ev)) != 1 {
		t.Fatalf("new active cand: want 1 deliver, got %v", ev)
	}
	// 重放已生效协商的 offer/answer 不产生第二份输出。
	if ev := c.Submit(offer(1, 2, 1, 2, "o3")); len(ev) != 0 {
		t.Fatalf("replayed active offer: want no events, got %v", ev)
	}
	if ev := c.Submit(answer(2, 1, 1, 2, "a3")); len(ev) != 0 {
		t.Fatalf("replayed active answer: want no events, got %v", ev)
	}
}

// 场景二：候选、结束标记先于描述到达，有缺号和重复；
// 补齐后各方向输出须与完整编号序列逐项一致，描述在前、结束标记在后。
func TestEarlyOutOfOrderCandidates(t *testing.T) {
	c := NewCoordinator([]int{1, 2})

	// offer 未到时：结束标记、乱序候选（缺 1 号）、重复候选先到。
	for _, s := range []Signal{
		end(1, 2, 1, 1, 3),
		cand(1, 2, 1, 1, 2, "i2"),
		cand(1, 2, 1, 1, 3, "i3"),
		cand(1, 2, 1, 1, 2, "i2"), // 重复
		cand(1, 2, 1, 1, 1, "i1"),
	} {
		if ev := c.Submit(s); len(ev) != 0 {
			t.Fatalf("before offer: want no events, got %v", ev)
		}
	}

	// offer 到达：先转交 offer，再按 1,2,3 顺序转交候选，最后是结束标记。
	ev := c.Submit(offer(1, 2, 1, 1, "o1"))
	got := delivered(ev)
	want := []Signal{
		offer(1, 2, 1, 1, "o1"),
		cand(1, 2, 1, 1, 1, "i1"),
		cand(1, 2, 1, 1, 2, "i2"),
		cand(1, 2, 1, 1, 3, "i3"),
		end(1, 2, 1, 1, 3),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("initiator dir: want %v, got %v", want, got)
	}

	// 接收方候选在 answer 转交前只能保留：2 号先到，缺 1 号不能越过。
	for _, s := range []Signal{
		cand(2, 1, 1, 1, 2, "r2"),
		end(2, 1, 1, 1, 2),
	} {
		if ev := c.Submit(s); len(ev) != 0 {
			t.Fatalf("before answer: want no events, got %v", ev)
		}
	}
	// answer 转交，但缺 1 号，2 号与结束标记仍须保留。
	ev = c.Submit(answer(2, 1, 1, 1, "a1"))
	if got := delivered(ev); !reflect.DeepEqual(got, []Signal{answer(2, 1, 1, 1, "a1")}) {
		t.Fatalf("answer: want only answer, got %v", got)
	}
	// 补齐 1 号后：按 1,2 顺序转交，再输出结束标记。
	ev = c.Submit(cand(2, 1, 1, 1, 1, "r1"))
	got = delivered(ev)
	want = []Signal{
		cand(2, 1, 1, 1, 1, "r1"),
		cand(2, 1, 1, 1, 2, "r2"),
		end(2, 1, 1, 1, 2),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("receiver dir: want %v, got %v", want, got)
	}

	// 重复提交候选与结束标记不产生第二份输出。
	for _, s := range []Signal{
		cand(1, 2, 1, 1, 1, "i1"),
		cand(2, 1, 1, 1, 1, "r1"),
		end(1, 2, 1, 1, 3),
		end(2, 1, 1, 1, 2),
	} {
		if ev := c.Submit(s); len(ev) != 0 {
			t.Fatalf("duplicate: want no events, got %v", ev)
		}
	}
}

// 附加：N=0 的结束标记、成员对之间互不影响、未知成员被忽略。
func TestMisc(t *testing.T) {
	c := NewCoordinator([]int{1, 2, 3})

	// N=0 的结束标记先于 offer 到达，offer 转交后立即输出。
	if ev := c.Submit(end(1, 2, 1, 1, 0)); len(ev) != 0 {
		t.Fatalf("end before offer: want no events, got %v", ev)
	}
	ev := c.Submit(offer(1, 2, 1, 1, "o1"))
	if got := delivered(ev); !reflect.DeepEqual(got, []Signal{offer(1, 2, 1, 1, "o1"), end(1, 2, 1, 1, 0)}) {
		t.Fatalf("N=0: want offer then end, got %v", got)
	}

	// 同一成员在另一对成员中使用相同协商标识，互不影响。
	if ev := c.Submit(offer(1, 3, 1, 1, "o1-13")); len(delivered(ev)) != 1 {
		t.Fatalf("offer on pair (1,3): want 1 deliver, got %v", ev)
	}
	if ev := c.Submit(cand(1, 3, 1, 1, 1, "c13")); len(delivered(ev)) != 1 {
		t.Fatalf("cand on pair (1,3): want 1 deliver, got %v", ev)
	}
	// pair (1,2) 上 {1,1} 仍是待协商，其接收方候选不能转交。
	if ev := c.Submit(cand(2, 1, 1, 1, 1, "c12")); len(ev) != 0 {
		t.Fatalf("pair (1,2) receiver cand before answer: want no events, got %v", ev)
	}

	// 未知成员的信令被忽略。
	if ev := c.Submit(offer(1, 9, 1, 1, "x")); len(ev) != 0 {
		t.Fatalf("unknown member: want no events, got %v", ev)
	}
}
