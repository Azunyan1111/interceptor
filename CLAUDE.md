# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト概要

Pion Interceptorは、RTP/RTCPパケットを処理するためのGoフレームワークです。WebRTCなどのリアルタイム通信ソフトウェアを構築する際に、パケットの検査、変更、独自パケットの送信を可能にするミドルウェア層を提供します。

## ビルドとテスト

```bash
# テスト実行
go test ./...

# 特定パッケージのテスト
go test ./pkg/nack/...

# 単一テスト実行
go test -run TestFunctionName ./pkg/nack/

# ベンチマーク
go test -bench=. ./pkg/flexfec/
```

## アーキテクチャ

### コアインターフェース (`interceptor.go`)

`Interceptor`インターフェースが全体の中心です:
- `BindRTCPReader/Writer` - RTCP パケットの検査/変更
- `BindLocalStream/BindRemoteStream` - RTP ストリームへのバインド
- `UnbindLocalStream/UnbindRemoteStream` - ストリーム解除時のクリーンアップ

### 主要コンポーネント

- **NoOp** (`noop.go`): 何もしないベースインターセプター。カスタムインターセプター作成時に埋め込み可能
- **Chain** (`chain.go`): 複数のインターセプターを順次実行するためのチェーン
- **Registry** (`registry.go`): `Factory`パターンでインターセプターを登録・構築
- **Attributes** (`attributes.go`): インターセプター間でメタデータを受け渡すKey-Valueストア

### 提供されているインターセプター (`pkg/`配下)

| パッケージ | 機能 |
|-----------|------|
| `nack` | NACK(再送要求)の生成と応答 |
| `report` | Sender/Receiver Reportの生成 |
| `twcc` | Transport-Wide Congestion Control |
| `gcc` | Google Congestion Control |
| `flexfec` | FlexFEC-03 エンコーダー |
| `jitterbuffer` | パケット並べ替えと待機 |
| `intervalpli` | 定期的なPLI生成 |
| `packetdump` | パケットダンプ(デバッグ用) |
| `stats` | WebRTC Stats準拠の統計生成 |
| `rfc8888` | RTCP Feedback for Congestion Control |

### カスタムインターセプター作成パターン

1. `interceptor.NoOp`を埋め込む
2. 必要なメソッドのみオーバーライド
3. `Factory`インターフェースを実装して`Registry`に登録

```go
type MyInterceptorFactory struct {
    opts []MyOption
}

func (f *MyInterceptorFactory) NewInterceptor(id string) (interceptor.Interceptor, error) {
    return &MyInterceptor{}, nil
}

type MyInterceptor struct {
    interceptor.NoOp
}

func (m *MyInterceptor) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
    // カスタム処理
    return writer
}
```

### 依存関係

- `github.com/pion/rtp` - RTPパケット処理
- `github.com/pion/rtcp` - RTCPパケット処理
- `github.com/pion/logging` - ロギング
- `github.com/pion/transport/v3` - トランスポート層ユーティリティ
