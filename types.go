// Package localcomm 提供 LocalComm 的 WebRTC 信令协调能力。
// 它不处理音视频数据，只负责把上层提交的信令整理成
// 可以按顺序直接转交的事件流。
package localcomm

// Kind 表示信令种类。
type Kind int

const (
	KindOffer Kind = iota
	KindAnswer
	KindCandidate
	KindEndOfCandidates
)

// NegID 是一次协商的标识：发起人用户编号 + 该成员对内单调递增的编号。
type NegID struct {
	Initiator int
	Seq       int
}

// Signal 是上层提交的一条信令。Payload 为不透明内容，原样转交。
type Signal struct {
	Kind Kind
	From int // 发送人用户编号
	To   int // 成员对另一方用户编号
	Neg  NegID

	CandSeq   int // KindCandidate：该发送方向上的候选编号（从 1 开始）
	CandTotal int // KindEndOfCandidates：该方向候选总数 N（可为 0）
	Payload   []byte
}

// EventKind 表示输出事件种类。
type EventKind int

const (
	// EventDeliver 表示把 Signal 原样转交给 To。
	EventDeliver EventKind = iota
	// EventInvalidate 表示 Neg 标识的协商已作废，通知 To。
	EventInvalidate
)

// Event 是协调器产出的待转交事件。
type Event struct {
	Kind   EventKind
	To     int
	Signal Signal // EventDeliver 时有效
	Neg    NegID  // EventInvalidate 时有效
}
