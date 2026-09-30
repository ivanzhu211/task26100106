# task26100106

LocalComm 的 WebRTC 信令协调库（Go），由上层直接调用，状态保存在内存中。
不处理音视频数据，只负责把提交的信令整理成可按顺序直接转交的事件。

## 用法

```go
c := localcomm.NewCoordinator([]int{1, 2, 3}) // 房间成员，运行期间不变
events := c.Submit(localcomm.Signal{          // 提交一条信令
    Kind: localcomm.KindOffer,
    From: 1, To: 2,
    Neg:  localcomm.NegID{Initiator: 1, Seq: 1},
    Payload: sdp, // 不透明内容，原样转交
})
// events 为本次待转交事件：EventDeliver（转交信令）/ EventInvalidate（协商作废通知）
```

## 行为要点

- 每对成员独立协商：至多一个已生效方案 + 一个待协商方案。
- 新 offer 不替换已生效方案；对应 answer 到达才转交并生效，此后旧方案信令全部失效。
- 待协商期间 offer 碰撞：发起人用户编号较小者获胜；被淘汰方向双方各产生一次作废通知，且排在获胜 offer 之前。
- ICE 候选按发送方向编号 1..N 严格按序、恰好转交一次；缺号保留后续候选；结束标记在描述与全部 N 个候选之后输出且恰好一次。
- 候选/结束标记可先于描述到达（暂存）；重复提交不产生第二份输出；被淘汰/被替换协商的迟到内容永久丢弃。

## 测试

```sh
go test ./...
```
