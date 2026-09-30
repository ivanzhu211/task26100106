package localcomm

// dirState 跟踪协商中一个发送方向上的候选转交进度。
type dirState struct {
	next     int            // 下一个待转交候选编号，从 1 开始
	buffered map[int]Signal // 乱序到达、暂不能转交的候选
	endKnown bool           // 是否已收到结束标记
	endSig   Signal         // 结束标记原样内容
	endSent  bool           // 结束标记是否已转交
}

func newDirState() *dirState {
	return &dirState{next: 1, buffered: map[int]Signal{}}
}

// negState 是一次协商（offer/answer/ICE）的运行状态。
type negState struct {
	id           NegID
	receiver     int // 成员对中非发起人的一方
	offerSent    bool
	answerSent   bool
	initiatorDir *dirState
	receiverDir  *dirState
}

func newNegState(id NegID, receiver int) *negState {
	return &negState{
		id:           id,
		receiver:     receiver,
		initiatorDir: newDirState(),
		receiverDir:  newDirState(),
	}
}

// peer 返回成员对中 sender 的另一方。
func (n *negState) peer(sender int) int {
	if sender == n.id.Initiator {
		return n.receiver
	}
	return n.id.Initiator
}

// dirOf 返回发送人对应的方向状态。
func (n *negState) dirOf(sender int) *dirState {
	if sender == n.id.Initiator {
		return n.initiatorDir
	}
	return n.receiverDir
}

// descSent 报告该发送方向的描述（offer 或 answer）是否已转交。
func (n *negState) descSent(sender int) bool {
	if sender == n.id.Initiator {
		return n.offerSent
	}
	return n.answerSent
}

// addCandidate 记录一个候选；重复（已转交或已暂存）时忽略。
func (n *negState) addCandidate(s Signal) {
	d := n.dirOf(s.From)
	if s.CandSeq < d.next {
		return
	}
	if _, ok := d.buffered[s.CandSeq]; ok {
		return
	}
	d.buffered[s.CandSeq] = s
}

// addEnd 记录结束标记；重复时忽略。
func (n *negState) addEnd(s Signal) {
	d := n.dirOf(s.From)
	if d.endKnown {
		return
	}
	d.endKnown = true
	d.endSig = s
}

// flush 在描述已转交的前提下，按编号顺序转交该方向暂存的候选，
// 并在全部 N 个候选转交后转交结束标记。
func (n *negState) flush(sender int) []Event {
	d := n.dirOf(sender)
	if !n.descSent(sender) {
		return nil
	}
	var ev []Event
	for {
		s, ok := d.buffered[d.next]
		if !ok {
			break
		}
		delete(d.buffered, d.next)
		d.next++
		ev = append(ev, Event{Kind: EventDeliver, To: n.peer(sender), Signal: s})
	}
	if d.endKnown && !d.endSent && d.next-1 >= d.endSig.CandTotal {
		d.endSent = true
		ev = append(ev, Event{Kind: EventDeliver, To: n.peer(sender), Signal: d.endSig})
	}
	return ev
}
