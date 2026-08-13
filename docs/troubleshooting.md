# トラブルシューティング & FAQ ガイド

このガイドでは、`scenaremo` の利用中や開発中に発生しやすい問題とその解決手順をまとめています。

---

## 1. まず実行すること: `scenaremo doctor`

環境依存や必須ソフトウェアの欠如に関する問題は、`scenaremo doctor` コマンドで一括診断できます。

```bash
scenaremo doctor
```

```
[ OK ] Node.js: v24.18.0
[ OK ] pnpm: 11.11.0
[ NG ] renderer の依存: renderer/node_modules がありません
       → pnpm install --dir renderer を実行してください
[ NG ] VOICEVOX ENGINE: http://127.0.0.1:50021 に接続できませんでした
       → VOICEVOX アプリを起動してください
```

診断がエラーを返した場合は、出力される矢印（`→`）の解決アドバイスにそのまま従ってください。

---

## 2. 音声合成 (VOICEVOX) 関連のトラブル

### Q. VOICEVOX ENGINE に接続できない (`http://127.0.0.1:50021`)
**原因:** VOICEVOX のアプリケーションが起動していないか、別ポートで動作しています。

**対処法:**
1. **VOICEVOX GUI アプリを起動する**（アプリを起動している間、バックグラウンドでエンジンが有効化されます）。
2. **Docker で起動する:**
   ```bash
   docker run --rm -p 50021:50021 voicevox/voicevox_engine:cpu-latest
   ```
3. **別ポートで動いている場合:**
   `scenaremo build` や `scenaremo doctor` に `--voicevox-url` フラグを指定します。
   ```bash
   scenaremo doctor --voicevox-url=http://127.0.0.1:50030
   ```

---

## 3. レンダリング・Remotion 関連のトラブル

### Q. `staticFile()` で 404 エラー / 画像や音声が読み込めない
**原因:** Remotion 実行時の `--public-dir` 指定漏れ、または `props.json` 内に `./` で始まるパスが混入している可能性があります。

**対処法:**
- パスは `./assets/01.png` ではなく `assets/01.png` のように指定します（Remotion の `staticFile()` は `./` をエラーとみなします）。
- 直接 `remotion` コマンドを叩く場合は、`--public-dir` に動画ディレクトリ（`videos/ep01`）を渡しているか確認してください。

---

## 4. 音声・タイムライン関連のトラブル

### Q. 語尾がわずかに切れる気がする
**原因:** `scenaremo` のタイムライン切り上げ丸めルールにより音声末尾が切れることは原則ありませんが、プレイヤーやブラウザのフレーム境界での発声タイミングの影響でそう感じられる場合があります。

**対処法:**
- セリフの語尾に読点（`。` や `…`）を追加するか、`defaults.gapMs` / `defaults.sceneGapMs` を少し大きめ（例: `sceneGapMs: 300`）に設定してみてください。

### Q. シーン切り替え時のフェード（トランジション）で前の会話の語尾と重なる
**原因:** 既定のトランジション時間（400ms）に対して、`defaults.sceneGapMs`（既定 100ms + VOICEVOX無音 200ms = 300ms）が短いためです。

**対処法:**
`sceneGapMs` をトランジション時間より大きな値（例: `500`）に設定してください。
```yaml
defaults:
  sceneGapMs: 500  # トランジション(400ms)より大きくすることで無音の中でフェードが完結する
```

---

## 5. エディタ補完・検証関連のトラブル

### Q. VS Code で YAML の補完やリアルタイム検証が効かない
**原因:** YAML エクステンション (`redhat.vscode-yaml`) が未導入か、先頭のスキーマ宣言コメントが不足しています。

**対処法:**
1. VS Code 拡張機能 **YAML (redhat.vscode-yaml)** をインストールします。
2. `script.yaml` の 1 行目に以下のマジックコメントを記述します。
   ```yaml
   # yaml-language-server: $schema=../../docs/schema.json
   ```
