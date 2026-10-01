# Package Publisher

Package Publisher 是一個以 Go 實作的套件入庫工具，負責將已下載、已掃描並已核准的第三方套件發佈至 Azure DevOps Artifacts 或 Sonatype Nexus Repository。

目前支援 Maven、npm 與 PyPI，提供單套件及大量套件批次處理、內容衝突保護、SHA-256 驗證、重試、dry-run 與結構化 JSON／CSV 報告。

完整的安裝、操作步驟，以及 Maven、npm、PyPI 搭配 ADO、Nexus 的設定檔範例，請參閱[使用者操作手冊](docs/user-manual.zh-TW.md)。

> 目前定位是 Promotion／Publisher。

## 支援矩陣

| Format | `package.format` | `publish_driver` | 支援輸入 | 發佈工具 |
| --- | --- | --- | --- | --- |
| Maven | `maven` | `maven_cli` | POM + artifact、JAR-only、Maven repository directory | Maven CLI |
| npm | `npm` | `npm_cli` | `.tgz`、含 `package.json` 的目錄、`node_modules` | npm CLI |
| PyPI | `pypi` | `twine` | `.whl`、`.tar.gz`、`.zip` | Python + Twine |

目標 repository 可透過 `repositories.<profile>.provider` 選擇 `ado` 或 `nexus`。ADO Feed 可以同時存放不同套件格式；Nexus profile 應指向與套件格式相容的 hosted repository（例如 `maven2 (hosted)`、`npm (hosted)` 或 `pypi (hosted)`）。

## 發佈流程

每個套件會依序執行：

1. 偵測並解析本地套件。
2. 驗證套件座標、metadata 與必要檔案。
3. 計算每個檔案及整個 bundle 的 SHA-256。
4. 驗證所選 repository 連線與 credential。
5. 查詢遠端相同 name/version 是否存在。
6. 套用 existing package policy。
7. 執行 Maven、npm 或 Twine 發佈。
8. 從 repository 重新下載遠端檔案並比對 checksum。
9. 輸出單套件結果或 Batch Report。

Release 版本不會被覆蓋：相同版本若內容不同會直接失敗。

## 前置條件

- Go 1.25 或相容版本。
- 已存在且可寫入的 Azure DevOps Artifacts Feed，或格式相容的 Nexus hosted repository。
- ADO 使用具備 Packaging Read & Write 權限的 PAT；Nexus 使用具備 repository read/write 權限的帳號密碼。
- Maven 套件需要 Maven CLI（`mvn`）。
- npm 套件需要 Node.js 與 npm CLI；從 package directory 或 `node_modules` 建立 tarball 時需要 npm 11 以上。
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
  exclude:
    - quarantine
    - cache/legacy

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

使用 Nexus 時，將 profile 改為：

```yaml
repository_profile: nexus-maven

repositories:
  nexus-maven:
    provider: nexus
    base_url: https://nexus.example.com
    repository: maven-hosted
    username: publisher
    credential_ref: NEXUS_PASSWORD
```

`base_url` 是 Nexus server 的根路徑（若安裝於 context path，例：`https://host/nexus`，需包含該路徑），不要包含 `/repository/<name>`。密碼仍只透過 `credential_ref` 指定的環境變數提供。

加入 `--mode=test` 會進入離線推送模擬模式：

```bash
./package-publisher publish --config publisher.yaml --mode=test
```

此模式會忽略 `repository_profile` 與 `repositories`，因此設定檔可不包含 repository，也不需要 PAT。程式不會連線 Artifact Feed，亦不會執行 Maven、npm 或 Twine 推送；本地套件可成功解析與驗證時輸出 `SUCCESS`，否則輸出 `FAILED`。批次模式會逐項列出結果並彙總成功與失敗數量。

可用 `--output=json` 或 `--output=csv` 選擇結果格式；未指定時維持既有 JSON 格式。若要直接寫入檔案，加入與格式相符的 `--file`：

```bash
./package-publisher publish --config publisher.yaml --mode=test \
  --output=json --file=publish-result.json

./package-publisher publish --config publisher.yaml --mode=test \
  --output=csv --file=pkgfiles.csv
```

只指定 `--file` 時會由副檔名決定格式（`.json` 或 `.csv`）。使用 `--file` 後結果不會重複寫到 `stdout`。

設定 credential 並執行（依選用的 profile 設定其中一個）：

```bash
export ADO_ARTIFACT_PAT='your-secret-pat'
# export NEXUS_PASSWORD='your-secret-password'

go run ./cmd/publisher publish --config publisher.yaml
```

也可以先建置執行檔：

```bash
go build -o package-publisher ./cmd/publisher
./package-publisher publish --config publisher.yaml
```

需要在命令列查看設定載入、套件探索及逐項發佈狀態時，加入 `--verbose`：

```bash
./package-publisher publish --config publisher.yaml --verbose
```

進度訊息會寫入 `stderr`，最終 JSON 或 CSV 仍單獨寫入 `stdout`（或 `--file` 指定的檔案）。`--verbose` 亦可與 `--mode=test` 同時使用。

Maven/Gradle repository 中若可能包含只有 POM、沒有主 artifact 的套件，可加入 `--pomonly`：

```bash
./package-publisher publish --config publisher.yaml --pomonly
```

此旗標只適用於 `package.format: maven`，作用是允許缺少主 artifact 的 POM-only 套件，不會過濾檔案。若同目錄存在 JAR、Gradle `.module`、sources、javadoc 或其他 classifier artifact，仍會納入 bundle、checksum、遠端內容比對與發佈。未加入旗標時仍維持完整套件的既有檢查方式。

## 設定說明

### `package`

| 欄位 | 必要 | 說明 |
| --- | --- | --- |
| `path` | 與 `archive_path` 二選一 | 單一套件檔案、套件目錄或下載 repository root |
| `archive_path` | 與 `path` 二選一 | 外層封裝壓縮檔；支援 `.zip`、`.tar`、`.tar.gz`、`.tgz`，解壓後再探索及發佈 |
| `format` | 是 | `maven`、`npm`、`pypi` |
| `publish_driver` | 是 | 必須與 format 對應 |
| `recursive` | 否 | 遞迴探索並啟用 Batch Publish |
| `exclude` | 否 | 探索時略過的目錄清單；路徑相對於套件根目錄 |
| `maven.group_id` | 條件式 | JAR 沒有內嵌 Maven metadata 時使用 |
| `maven.artifact_id` | 條件式 | 必須與另外兩個 Maven fallback 欄位一起設定 |
| `maven.version` | 條件式 | 必須與另外兩個 Maven fallback 欄位一起設定 |

`path` 與 `archive_path` 支援 `${VAR}` 環境變數。例如：

```bash
export PACKAGE_ROOT="$(pwd)"
```

```yaml
package:
  path: "${PACKAGE_ROOT}/node_modules"
  format: npm
  publish_driver: npm_cli
  recursive: true
```

環境變數必須已設定且不可為空；建議 `PACKAGE_ROOT` 使用絕對路徑。此功能不會展開 `credential_ref`，PAT 與密碼仍只填環境變數名稱。

`exclude` 可在 Publisher 探索套件時略過指定目錄及其所有子目錄：

```yaml
package:
  path: ./downloaded-packages
  format: npm
  publish_driver: npm_cli
  recursive: true
  exclude:
    - quarantine
    - cache/legacy
```

每個項目都是相對於 `package.path` 的精確目錄路徑，不支援 glob；不可使用絕對路徑、`..` 或 `.`。使用 `archive_path` 時，路徑改為相對於封裝檔解壓後的根目錄，而且被排除的內容不會寫入暫存目錄。不存在的目錄不會造成錯誤。此設定不會排除單一 npm 套件內的檔案；後者應使用 `.npmignore` 或 `package.json` 的 `files` 欄位。

### 封裝壓縮檔

若掃描或核准流程將多個套件封裝為單一壓縮檔，可使用
`archive_path` 取代 `path`：

```yaml
package:
  archive_path: ./approved-packages.zip
  format: maven
  publish_driver: maven_cli
  recursive: true
```

Publisher 會將封裝檔解壓至暫存目錄，再沿用既有的套件探索、驗證與
Batch Publish 流程；執行結束後會移除暫存內容。為避免 Zip Slip 等攻擊，
壓縮檔內的絕對路徑、上層路徑、符號連結及特殊檔案都會被拒絕。
解壓時會直接略過 `._*`、`.DS_Store` 與 `__MACOSX` 內的 macOS metadata，不會將它們寫入暫存目錄。
`package.exclude` 指定的目錄也會在解壓時略過，因此排除目錄內的符號連結或特殊檔案不會觸發封裝檔安全檢查錯誤；未排除位置的符號連結仍會被拒絕。

`archive_path` 表示包住套件的「外層封裝」，不能和 `path` 同時設定。
npm 的 `.tgz` 或 PyPI 的 `.zip` 若本身就是待發佈套件，仍應使用 `path`。

### `repositories`

| 欄位 | 必要 | 說明 |
| --- | --- | --- |
| `provider` | 是 | `ado` 或 `nexus` |
| `organization` | ADO | Azure DevOps organization 名稱 |
| `project` | ADO／視 Feed 類型 | Project-scoped Feed 必填 |
| `feed` | ADO | 既有 Azure Artifacts Feed 名稱或 ID |
| `feed_base_url` | 否 | ADO Feed API 的 organization base URL；未設定時為 `https://feeds.dev.azure.com/<organization>` |
| `package_base_url` | 否 | ADO 套件查詢與發佈的 organization base URL；未設定時為 `https://pkgs.dev.azure.com/<organization>` |
| `base_url` | Nexus | Nexus server 根 URL，可包含安裝 context path |
| `repository` | Nexus | 格式相容的 Nexus hosted repository 名稱 |
| `username` | ADO 否／Nexus 是 | ADO Basic Auth username 或 Nexus 登入帳號；雲端 ADO 通常不需設定，ADO Server 可填 collection name |
| `credential_ref` | 是 | 保存 ADO PAT 或 Nexus 密碼的環境變數名稱，不是 secret 本身 |

### `options`

| 欄位 | 預設行為 | 說明 |
| --- | --- | --- |
| `existing_package_policy` | `SKIP_IDENTICAL` | 遠端內容相同則跳過，不同則失敗 |
| `timeout` | 不額外限制 | 單一套件 publish 與 verify timeout，例如 `5m` |
| `retry_count` | `0` | 發佈或驗證失敗後的額外重試次數 |
| `dry_run` | `false` | 解析、計算 checksum 並查詢遠端，但不執行上傳 |
| `parallelism` | `min(GOMAXPROCS, 8)` | Batch 同時工作的 worker 數量 |
| `fail_fast` | `false` | 第一個錯誤後取消尚未開始的 batch items |

`dry_run` 仍需要有效 credential，因為它會連線 repository 並查詢遠端狀態；套件前處理及 SHA sidecar 也會照常執行。

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
├── demo-1.0.0.module         # optional Gradle metadata
├── demo-1.0.0-sources.jar    # optional
└── demo-1.0.0-javadoc.jar    # optional
```

Handler 會解析 POM 的 groupId、artifactId、version 與 packaging，並將主 artifact、POM、Gradle `.module` 與 classifier artifacts（例如 sources、javadoc、tests）視為同一個 bundle。

### 只有 POM

只有 POM 的版本目錄可透過 `--pomonly` 放寬完整性檢查後發佈：

```bash
./package-publisher publish --config publisher.yaml --pomonly
```

此模式也支援 recursive batch；每個版本目錄仍只能有一個 POM。`--pomonly` 不會強制只發佈 POM：目錄內若有主 JAR、`.module` 或 classifier artifacts，仍會全部發佈。若未加入 `--pomonly`，且 POM 的 packaging 不是 `pom`，缺少主 artifact 時仍會失敗。

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

此模式需要 npm 11 以上；npm 10 及更早版本的 `npm pack` 不會讓
`--ignore-scripts` 阻止 `prepare`，工具會先拒絕執行並提示升級。若無法升級，請改以預先建立的 `.tgz` 作為輸入。

`private: true` 的套件會在本地拒絕。Recursive discovery 會辨識各層 `node_modules` 中真正的套件根目錄、忽略 fixture，並依 name/version 去重。

npm 設定只需要替換 `package` 區塊：

```yaml
package:
  path: ./downloaded-npm-repository
  format: npm
  publish_driver: npm_cli
  recursive: true
  npm:
    tag: legacy
```

`package.npm.tag` 會以 `npm publish --tag <tag>` 傳入。回補低於遠端
`latest` 的舊版本時，可設定 `legacy`、`v4` 等非 `latest` dist-tag；
未設定時仍沿用 npm 的預設 `latest`。

## PyPI

PyPI 支援：

- Wheel：`.whl`
- Source distribution：`.tar.gz`、`.zip`

套件名稱與版本直接取自 wheel 的 `METADATA` 或 sdist 的 `PKG-INFO`，不以檔名猜測。名稱會依 Python packaging 規則正規化。

探索時會忽略檔名或目錄名稱以 `.` 開頭的隱藏項目，包括 macOS 壓縮檔常見的 `._` AppleDouble metadata；這些項目不會嘗試發佈，也不會出現在 CSV 清單。

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

單套件輸出 `PublishResult`，Batch 輸出 `BatchPublishReport`，可透過 `--output=json|csv` 選擇格式；預設為 JSON。這些參數同時適用於正式發佈與 `--mode=test`。若使用 `--file`，副檔名必須與輸出格式一致，結果只寫入該檔案。

CSV 用於稽核套件內的實體檔案，包含 `correlationId`、`status`、`format`、`name`、`version`、`fileName`、`filePath` 與 `fileSha256` 八欄，每個實體檔案各占一列。`status` 為該檔案所屬套件的發佈結果，可能為 `SUCCESS`、`SKIPPED` 或 `FAILED`。若同一套件含多個檔案（例如 PyPI 同版本的 wheel 與 sdist），每個檔案都會分別輸出；若套件在檔案清單建立前即失敗，CSV 只會包含標頭。範例如下：

```csv
correlationId,status,format,name,version,fileName,filePath,fileSha256
promotion-001,SUCCESS,npm,demo,1.0.0,demo-1.0.0.tgz,/packages/demo-1.0.0.tgz,abc123...
```

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

## License

本專案採用 [MIT License](LICENSE)，Copyright (c) 2026 Jonas Yang。
