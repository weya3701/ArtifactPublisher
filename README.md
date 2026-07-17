# Package Publisher

Package Publisher 是一個以 Go 實作的套件入庫工具，負責將已下載、已掃描並已核准的第三方套件發佈至 Azure DevOps Artifacts Feed。

目前支援 Maven、npm 與 PyPI，提供單套件及大量套件批次處理、內容衝突保護、SHA-256 驗證、重試、dry-run 與結構化 JSON 報告。

> 目前定位是 Promotion／Publisher。

## 支援矩陣

| Format | `package.format` | `publish_driver` | 支援輸入 | 發佈工具 |
| --- | --- | --- | --- | --- |
| Maven | `maven` | `maven_cli` | POM + artifact、JAR-only、Maven repository directory | Maven CLI |
| npm | `npm` | `npm_cli` | `.tgz`、含 `package.json` 的目錄、`node_modules` | npm CLI |
| PyPI | `pypi` | `twine` | `.whl`、`.tar.gz`、`.zip` | Python + Twine |

目前目標 repository 僅支援 Azure DevOps Artifacts。ADO Feed 可以同時存放不同套件格式，不需要為 Maven、npm 與 PyPI 強制建立不同 Feed。

## 發佈流程

每個套件會依序執行：

1. 偵測並解析本地套件。
2. 驗證套件座標、metadata 與必要檔案。
3. 計算每個檔案及整個 bundle 的 SHA-256。
4. 驗證 ADO Feed 連線與 PAT。
5. 查詢遠端相同 name/version 是否存在。
6. 套用 existing package policy。
7. 執行 Maven、npm 或 Twine 發佈。
8. 從 ADO 重新下載遠端檔案並比對 checksum。
9. 輸出單套件結果或 Batch Report。

Release 版本不會被覆蓋：相同版本若內容不同會直接失敗。

## 前置條件

- Go 1.25 或相容版本。
- 已存在且可寫入的 Azure DevOps Artifacts Feed。
- 具備 Packaging Read & Write 權限的 Azure DevOps PAT。
- Maven 套件需要 Maven CLI（`mvn`）。
- npm 套件需要 Node.js 與 npm CLI。
- PyPI 套件需要 Python 3 與 Twine。

安裝 PyPI 發佈工具：

```bash
python3 -m pip install twine
```

## 快速開始

建立 `publisher.yaml`：

```yaml
package:
  path: ./downloaded-maven-repository
  format: maven
  publish_driver: maven_cli
  recursive: true

repository_profile: internal-packages

repositories:
  internal-packages:
    provider: ado
    organization: your-organization
    project: your-project
    feed: your-feed
    credential_ref: ADO_ARTIFACT_PAT

options:
  existing_package_policy: SKIP_IDENTICAL
  timeout: 5m
  retry_count: 2
  dry_run: false
  parallelism: 8
  fail_fast: false

metadata:
  pipeline_id: ""
  build_id: ""
  commit_sha: ""
  correlation_id: ""
```

`repository_profile` 必須與 `repositories` 下的 key 完全一致。Project-scoped Feed 必須填寫 `project`；organization-scoped Feed 可使用空字串。

設定 PAT 並執行：

```bash
export ADO_ARTIFACT_PAT='your-secret-pat'

go run ./cmd/publisher publish --config publisher.yaml
```

也可以先建置執行檔：

```bash
go build -o package-publisher ./cmd/publisher
./package-publisher publish --config publisher.yaml
```

## 設定說明

### `package`

| 欄位 | 必要 | 說明 |
| --- | --- | --- |
| `path` | 是 | 單一套件檔案、套件目錄或下載 repository root |
| `format` | 是 | `maven`、`npm`、`pypi` |
| `publish_driver` | 是 | 必須與 format 對應 |
| `recursive` | 否 | 遞迴探索並啟用 Batch Publish |
| `maven.group_id` | 條件式 | JAR 沒有內嵌 Maven metadata 時使用 |
| `maven.artifact_id` | 條件式 | 必須與另外兩個 Maven fallback 欄位一起設定 |
| `maven.version` | 條件式 | 必須與另外兩個 Maven fallback 欄位一起設定 |

### `repositories`

| 欄位 | 必要 | 說明 |
| --- | --- | --- |
| `provider` | 是 | 目前只支援 `ado` |
| `organization` | 是 | Azure DevOps organization 名稱 |
| `project` | 視 Feed 類型 | Project-scoped Feed 必填 |
| `feed` | 是 | 既有 Azure Artifacts Feed 名稱或 ID |
| `credential_ref` | 是 | 保存 PAT 的環境變數名稱，不是 PAT 本身 |

### `options`

| 欄位 | 預設行為 | 說明 |
| --- | --- | --- |
| `existing_package_policy` | `SKIP_IDENTICAL` | 遠端內容相同則跳過，不同則失敗 |
| `timeout` | 不額外限制 | 單一套件 publish 與 verify timeout，例如 `5m` |
| `retry_count` | `0` | 發佈或驗證失敗後的額外重試次數 |
| `dry_run` | `false` | 解析、計算 checksum 並查詢遠端，但不執行上傳 |
| `parallelism` | `min(GOMAXPROCS, 8)` | Batch 同時工作的 worker 數量 |
| `fail_fast` | `false` | 第一個錯誤後取消尚未開始的 batch items |

`dry_run` 仍需要有效 PAT，因為它會連線 ADO 並查詢遠端狀態；套件前處理及 SHA sidecar 也會照常執行。

### Existing package policy

| Policy | 遠端版本不存在 | 遠端 checksum 相同 | 遠端 checksum 不同 |
| --- | --- | --- | --- |
| `SKIP_IDENTICAL` | 發佈 | Skip | Fail |
| `FAIL_ON_CONFLICT` | 發佈 | Skip | Fail |
| `ALWAYS_FAIL_IF_EXISTS` | 發佈 | Fail | Fail |

## Maven

### 一般套件目錄

```text
downloaded-maven-repository/com/example/demo/1.0.0/
├── demo-1.0.0.jar
├── demo-1.0.0.pom
├── demo-1.0.0-sources.jar    # optional
└── demo-1.0.0-javadoc.jar    # optional
```

Handler 會解析 POM 的 groupId、artifactId、version 與 packaging，並將主 artifact、POM、sources、javadoc 視為同一個 bundle。

### 只有 JAR

如果目錄中只有一個主 JAR，程式會：

1. 讀取 `META-INF/maven/**/pom.properties`。
2. 驗證 JAR 檔名與 GAV 一致。
3. 產生最小 POM。
4. 為 JAR 與 POM 產生 `.sha256`。
5. 再執行一般發佈流程。

沒有內嵌 metadata 時，單套件設定可以提供完整 fallback GAV：

```yaml
package:
  path: ./artifacts/legacy-4.5.6.jar
  format: maven
  publish_driver: maven_cli
  recursive: false
  maven:
    group_id: org.legacy
    artifact_id: legacy
    version: 4.5.6
```

Recursive batch 不建議使用固定 fallback GAV，否則不同 JAR 可能被套用同一組座標。

## npm

npm 支援：

- 已封裝的 `.tgz`，內部必須包含 `package/package.json`。
- 含 `package.json` 的 package directory。
- npm install 產生的 `node_modules` repository。
- Scoped package，例如 `@company/demo`。

若輸入是 package directory 且沒有 `.tgz`，程式會執行：

```bash
npm pack <package-directory> --json --ignore-scripts
```

`private: true` 的套件會在本地拒絕。Recursive discovery 會辨識各層 `node_modules` 中真正的套件根目錄、忽略 fixture，並依 name/version 去重。

npm 設定只需要替換 `package` 區塊：

```yaml
package:
  path: ./downloaded-npm-repository
  format: npm
  publish_driver: npm_cli
  recursive: true
```

## PyPI

PyPI 支援：

- Wheel：`.whl`
- Source distribution：`.tar.gz`、`.zip`

套件名稱與版本直接取自 wheel 的 `METADATA` 或 sdist 的 `PKG-INFO`，不以檔名猜測。名稱會依 Python packaging 規則正規化。

同一目錄內相同 name/version 的 wheel 與 sdist 會合併為一個 bundle，交由 Twine 一次發佈。Recursive discovery 會依正規化 name/version 去重，適合 `pip download` 產生的 flat directory。

下載範例：

```bash
mkdir -p downloaded-pypi-repository
python3 -m pip download --dest downloaded-pypi-repository requests flask
```

PyPI 設定只需要替換 `package` 區塊：

```yaml
package:
  path: ./downloaded-pypi-repository
  format: pypi
  publish_driver: twine
  recursive: true
```

## Batch Publish

當 `recursive: true`，CLI 會先探索所有套件，再使用有上限的 worker pool 執行入庫。即使輸入 1000 個套件，也只會建立 `parallelism` 指定數量的並行 publisher workers。

Batch 行為：

- 每個 worker 使用獨立 Publisher 與 repository adapter。
- 結果順序維持 discovery 順序。
- `fail_fast: false` 時，單一套件失敗不影響其他套件。
- `fail_fast: true` 時，第一個錯誤會取消尚未處理的項目。
- 已成功入庫的套件不會因後續項目失敗而 rollback。

## 輸出與 Exit Code

單套件輸出 `PublishResult`，Batch 輸出 `BatchPublishReport`。兩者均為 JSON，包含套件座標、checksum、repository、狀態、時間、錯誤類型與 metadata。

狀態：

- `SUCCESS`：發佈及遠端驗證成功。
- `SKIPPED`：相同內容已存在，或為 dry-run。
- `FAILED`：設定、套件、連線、衝突、發佈或驗證失敗。

錯誤類型包括：

- `CONFIGURATION`
- `PACKAGE`
- `CONNECTION`
- `CONFLICT`
- `PUBLISH`
- `VERIFICATION`
- `WORKER_INIT`
- `CANCELLED`

CLI Exit Code：

| Code | 意義 |
| ---: | --- |
| `0` | 全部成功或安全跳過 |
| `1` | 套件處理、發佈或 Batch 中至少一項失敗 |
| `2` | CLI 參數、YAML 設定或 credential 初始化失敗 |

## 檔案副作用

Publisher 在套件目錄中可能建立下列檔案：

- Maven JAR-only：產生最小 `.pom`。
- npm package directory：產生 `.tgz`。
- Maven、npm、PyPI：為待發佈檔案產生 `.sha256` sidecar。

如果下載目錄必須保持唯讀，應先複製到 promotion workspace 再執行。

## Library API

其他 Go 程式可以使用內建 constructors：

```go
import publisher "packagespublisher/pkg/publisher"

service, err := publisher.NewPyPIADOService(publisher.PyPIADOConfig{
    Organization:     "your-organization",
    Project:          "your-project",
    Feed:             "your-feed",
    PAT:              secretFromYourSecretProvider,
    PythonExecutable: "python3",
})
if err != nil {
    return err
}

result, err := service.Publish(ctx, publisher.PublishRequest{
    PackagePath: "./dist/demo-1.0.0-py3-none-any.whl",
    Options: publisher.PublishOptions{
        ExistingPackagePolicy: publisher.PolicySkipIdentical,
    },
})
```

可用 constructors：

- `NewMavenADOService`
- `NewNPMADOService`
- `NewPyPIADOService`

也可以透過 `NewService` 注入自訂 `PackageHandler`、`ArtifactRepository` 與 `PublishDriver`。

## 專案結構

```text
cmd/publisher/                    CLI 入口
internal/model/                   核心資料模型與 checksum
internal/publisher/               單套件流程與 Batch worker pool
internal/package/discovery/       Maven、npm、PyPI 套件探索
internal/package/formats/         各格式 Handler
internal/package/drivers/         Maven CLI、npm CLI、Twine drivers
internal/artifact_repository/     Repository port、credential 與 ADO adapter
internal/infrastructure/          YAML config 與 environment secret
internal/bootstrap/               依設定組合 adapters
pkg/publisher/                    對外 Go Library API
architect.md                      架構與演進規劃
```

## 安全性

- PAT 不應寫入 YAML、Git 或 JSON result。
- Maven 使用權限 `0600` 的臨時 settings。
- npm 使用權限 `0600` 的臨時 `.npmrc`。
- Twine credential 透過執行環境傳遞，不放入 CLI arguments。
- Driver 錯誤輸出會遮蔽已知 PAT。
- Release 同版本不同內容永遠拒絕，不支援 overwrite。

## 測試與驗證

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

測試目前涵蓋：

- Maven/npm/PyPI metadata 與完整性。
- JAR-only POM 產生。
- npm pack 與 PyPI wheel/sdist grouping。
- ADO Maven/npm/PyPI REST path 與遠端 checksum。
- PAT 不出現在 Maven/npm/Twine 命令參數。
- Retry、conflict、dry-run 與發佈後驗證。
- Batch bounded parallelism、順序、fail-fast 與 worker 初始化失敗。

## 目前限制與下一步

- Repository provider 僅支援 Azure DevOps Artifacts。
- 尚未整合 compliance evidence 或 scanner decision。
- Retry 尚未加入 exponential backoff 與錯誤分類。
- 尚未支援跨執行 Resume、Promotion History 與 persistent audit sink。
- 尚未封裝為 ADO Pipeline Task 或 container image。
- 尚未對真實 ADO Feed 執行自動化 end-to-end test；目前 ADO 行為由 HTTP contract tests 驗證。

## License

本專案採用 [MIT License](LICENSE)，Copyright (c) 2026 Jonas Yang。
