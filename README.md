# localcomm

LocalComm 的 WebRTC 信令协调库（纯内存，不处理音视频数据）。
上层逐条提交信令，拿回本次需要按顺序转交的事件。

## 用法

```go
c := localcomm.New([]int{1, 2, 3}) // 房间成员，运行期间不变
events := c.Submit(localcomm.Signal{
    Kind: localcomm.KindOffer,
    From: 1, To: 2,
    Neg:  localcomm.NegotiationID{Initiator: 1, Round: 1},
    Payload: "sdp...", // 不透明载荷，原样转交
})
// events 按顺序直接转交即可
```

## 语义

- 每对成员独立协商：至多一个已生效方案 + 一个待协商方案。
- 新 offer 不替换已生效方案；对应 answer 转交后新方案才生效，旧方案信令随即全部失效。
- 双方 offer 碰撞时发起人用户编号较小者获胜；被淘汰方向双方各产生一次 `EventInvalidate`，且排在获胜 offer 之前。
- ICE 候选按发送方向编号（从 1 连续递增），只有对应描述（offer/answer）转交后才按序放行，缺号不越过；结束标记在该方向全部 N 个候选转交后输出，恰好一次。
- 候选与结束标记可任意乱序、可先于描述到达；重复提交不产生第二份输出；已失效协商的迟到内容一律丢弃。

## 测试

```sh
go test ./...
```
