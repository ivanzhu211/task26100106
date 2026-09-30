// Package localcomm 提供 WebRTC 信令协调能力的内存实现。
// 它不处理音视频数据，只负责把上层提交的信令整理成
// 可以按顺序直接转交的事件流。
package localcomm

// Kind 表示信令种类。
type Kind int

const (
	KindOffer Kind = iota
	KindAnswer
	KindCandidate
	KindCandidatesEnd
)

// NegotiationID 唯一标识一对成员间的一轮协商，
// 由发起人用户编号和该成员对内单调递增的编号组成。
type NegotiationID struct {
	Initiator int
	Round     int
}

// Signal 是上层提交的一条信令。描述与候选的载荷
// 对本库而言是不透明内容，转交时原样保留。
type Signal struct {
	Kind    Kind
	From    int
	To      int
	Neg     NegotiationID
	Seq     int    // 候选编号，仅 KindCandidate 有效，从 1 开始
	Count   int    // 该方向候选总数，仅 KindCandidatesEnd 有效
	Payload string // 不透明载荷
}

// EventKind 表示待转交事件的种类。
type EventKind int

const (
	EventOffer EventKind = iota
	EventAnswer
	EventCandidate
	EventCandidatesEnd
	EventInvalidate // 协商被碰撞淘汰，通知双方作废
)

// Event 是一条可以直接转交的事件。
type Event struct {
	Kind   EventKind
	To     int
	Signal Signal // Kind 为 EventInvalidate 时仅 Neg 字段有意义
}
