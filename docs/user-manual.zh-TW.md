# Artifact Publisher 使用者操作手冊

本手冊說明如何使用 Artifact Publisher，將 Maven、npm 與 PyPI 套件發佈至 Azure DevOps Artifacts（以下簡稱 ADO）或 Sonatype Nexus Repository。內容以 CLI 操作為主，所有範例均使用 YAML 設定檔。

## 1. 功能與支援範圍

| 套件類型 | `package.format` | `package.publish_driver` | 支援輸入 |
| --- | --- | --- | --- |
| Maven | `maven` | `maven_cli` | POM 與 artifact、單一 JAR、Maven repository 目錄 |
| npm | `npm` | `npm_cli` | `.tgz`、含 `package.json` 的目錄、`node_modules` |
| PyPI | `pypi` | `twine` | `.whl`、`.tar.gz`、`.zip` |

| 儲存庫 | `repositories.<名稱>.provider` | 認證方式 |
| --- | --- | --- |
| Azure DevOps Artifacts | `ado` | Personal Access Token（PAT） |
| Sonatype Nexus Repository | `nexus` | 使用者名稱與密碼 |
| 本機模擬 | 不需設定 repository | 不需認證 |

同一次執行只能選擇一種套件格式與一個 repository profile。ADO Feed 可以存放多種套件格式；Nexus profile 必須指向格式相容的 hosted repository。

## 2. 安裝與前置準備

### 2.1 建置執行檔

需求為 Go 1.25 或相容版本。

```bash
go build -o package-publisher ./cmd/publisher
```

確認 CLI 可啟動：

```bash
./package-publisher
```

若畫面顯示以下用法，表示執行檔可正常啟動：

```text
usage: package-publisher publish --config publisher.yaml
```

也可以不先建置，直接使用：

```bash
go run ./cmd/publisher publish --config publisher.yaml
```

### 2.2 安裝各格式所需工具

- Maven：安裝 `mvn`，並確認 `mvn --version` 可執行。
- npm：安裝 Node.js 與 npm，並確認 `npm --version` 可執行。
- PyPI：安裝 Python 3 與 Twine。

```bash
python3 -m pip install twine
python3 -m twine --version
```

### 2.3 準備儲存庫與權限

ADO：

- 建立或選擇既有 Azure Artifacts Feed。
- 建立具備 Packaging Read & Write 權限的 PAT。
- 確認 Feed 是 organization-scoped 或 project-scoped；後者必須在設定檔填寫 `project`。

Nexus：

- 依套件類型建立 `maven2 (hosted)`、`npm (hosted)` 或 `pypi (hosted)` repository。
- 準備具備該 repository 讀取及寫入權限的帳號。
- 同一個 Nexus repository 不應混用不相容的套件格式。

## 3. 執行步驟

每次發佈的基本流程如下：

1. 準備套件檔案或套件目錄。
2. 建立 `publisher.yaml`。
3. 將密碼或 PAT 放入環境變數。
4. 視需要先執行本機模擬或 `dry_run`。
5. 執行正式發佈。
6. 檢查 JSON 結果及 exit code。

正式執行：

```bash
./package-publisher publish --config publisher.yaml
```

如需保留報告：

```bash
./package-publisher publish --config publisher.yaml > publish-result.json
```

## 4. YAML 設定檔結構

完整設定檔包含五個頂層區塊：

```yaml
package:
  path: ./packages
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

### 4.1 `package`

| 欄位 | 必要 | 值或說明 |
| --- | --- | --- |
| `path` | 與 `archive_path` 二選一 | 套件檔案、套件目錄或 repository root |
| `archive_path` | 與 `path` 二選一 | 包住套件的外層 `.zip`、`.tar`、`.tar.gz` 或 `.tgz` |
| `format` | 是 | `maven`、`npm`、`pypi` |
| `publish_driver` | 是 | Maven=`maven_cli`、npm=`npm_cli`、PyPI=`twine` |
| `recursive` | 否 | `true` 表示遞迴探索並採批次模式 |
| `maven.*` | 條件式 | 無 metadata 的單一 JAR 所需 fallback GAV |
| `npm.tag` | 否 | 發佈時指定 npm dist-tag |

`path` 與 `archive_path` 不可同時出現。路徑可為相對於執行時工作目錄的相對路徑，也可使用絕對路徑。

### 4.2 `repository_profile` 與 `repositories`

`repository_profile` 是本次使用的 profile 名稱，必須與 `repositories` 下方的 key 完全相同：

```yaml
repository_profile: production

repositories:
  production:
    provider: ado
    # ...
```

同一份設定檔可預先放置多個 profile，再切換 `repository_profile`：

```yaml
repository_profile: ado-production

repositories:
  ado-production:
    provider: ado
    organization: contoso
    project: platform
    feed: approved-packages
    credential_ref: ADO_PROD_PAT

  nexus-dr:
    provider: nexus
    base_url: https://nexus.example.com
    repository: maven-hosted
    username: publisher
    credential_ref: NEXUS_DR_PASSWORD
```

只有被選取的 profile 會使用其 credential。

### 4.3 `options`

| 欄位 | 預設行為 | 說明 |
| --- | --- | --- |
| `existing_package_policy` | `SKIP_IDENTICAL` | 相同版本已存在時的處理方式 |
| `timeout` | 不額外限制 | 單一套件 publish 與 verify 的 Go duration，例如 `30s`、`5m` |
| `retry_count` | `0` | 發佈或驗證失敗後的額外重試次數 |
| `dry_run` | `false` | 查詢遠端但不上傳 |
| `parallelism` | `min(GOMAXPROCS, 8)` | 批次 worker 數；`0` 使用預設值 |
| `fail_fast` | `false` | 第一個錯誤後取消尚未開始的項目 |

Existing package policy：

| Policy | 遠端不存在 | checksum 相同 | checksum 不同 |
| --- | --- | --- | --- |
| `SKIP_IDENTICAL` | 發佈 | 跳過 | 失敗 |
| `FAIL_ON_CONFLICT` | 發佈 | 跳過 | 失敗 |
| `ALWAYS_FAIL_IF_EXISTS` | 發佈 | 失敗 | 失敗 |

本工具不會覆蓋內容不同的相同 release 版本。

`dry_run: true` 仍會連線並查詢遠端，因此仍需有效 credential。它也會解析套件、計算 checksum，並可能建立本機暫存產物或 `.sha256` sidecar。

### 4.4 `metadata`

`metadata` 會原樣出現在 JSON 結果，方便與 CI/CD 執行紀錄關聯；欄位可留空或整段省略。

```yaml
metadata:
  pipeline_id: release-packages
  build_id: "20260731.4"
  commit_sha: 0123456789abcdef
  correlation_id: promotion-20260731-001
```

## 5. Azure DevOps Artifacts 設定

### 5.1 Organization-scoped Feed

Organization-scoped Feed 的 `project` 留空：

```yaml
repository_profile: ado-org-feed

repositories:
  ado-org-feed:
    provider: ado
    organization: contoso
    project: ""
    feed: approved-packages
    credential_ref: ADO_ARTIFACT_PAT
```

### 5.2 Project-scoped Feed

Project-scoped Feed 必須填寫 `project`：

```yaml
repository_profile: ado-project-feed

repositories:
  ado-project-feed:
    provider: ado
    organization: contoso
    project: platform
    feed: approved-packages
    credential_ref: ADO_ARTIFACT_PAT
```

設定 PAT。`credential_ref` 填的是環境變數名稱，不是 PAT 本身：

```bash
export ADO_ARTIFACT_PAT='replace-with-your-pat'
```

ADO profile 必填欄位：

| 欄位 | 說明 |
| --- | --- |
| `provider` | 固定為 `ado` |
| `organization` | Azure DevOps organization 名稱 |
| `project` | Project-scoped Feed 的 project；organization-scoped 留空 |
| `feed` | Feed 名稱或 ID |
| `credential_ref` | 保存 PAT 的環境變數名稱 |

`base_url` 通常省略。它主要供自訂或測試端點使用，正常 Azure DevOps Services 會依 `organization` 自動組合 URL。

## 6. Nexus Repository 設定

每一種套件格式應建立個別 profile，並各自指向相容的 hosted repository：

```yaml
repository_profile: nexus-maven

repositories:
  nexus-maven:
    provider: nexus
    base_url: https://nexus.example.com
    repository: maven-hosted
    username: publisher
    credential_ref: NEXUS_PASSWORD

  nexus-npm:
    provider: nexus
    base_url: https://nexus.example.com
    repository: npm-hosted
    username: publisher
    credential_ref: NEXUS_PASSWORD

  nexus-pypi:
    provider: nexus
    base_url: https://nexus.example.com
    repository: pypi-hosted
    username: publisher
    credential_ref: NEXUS_PASSWORD
```

設定密碼：

```bash
export NEXUS_PASSWORD='replace-with-your-password'
```

Nexus profile 必填欄位：

| 欄位 | 說明 |
| --- | --- |
| `provider` | 固定為 `nexus` |
| `base_url` | Nexus server 根 URL |
| `repository` | hosted repository 名稱 |
| `username` | 發佈帳號 |
| `credential_ref` | 保存密碼的環境變數名稱 |

`base_url` 不可包含 `/repository/<repository-name>`。若 Nexus 安裝於 context path，則需保留該 path：

```yaml
base_url: https://nexus.example.com/nexus
repository: npm-hosted
```

工具會自動組合為 `https://nexus.example.com/nexus/repository/npm-hosted/`。

## 7. Maven 發佈

### 7.1 標準 Maven 套件目錄

建議輸入結構：

```text
downloaded-maven-repository/
└── com/example/demo/1.0.0/
    ├── demo-1.0.0.jar
    ├── demo-1.0.0.pom
    ├── demo-1.0.0-sources.jar
    └── demo-1.0.0-javadoc.jar
```

批次發佈至 ADO：

```yaml
package:
  path: ./downloaded-maven-repository
  format: maven
  publish_driver: maven_cli
  recursive: true

repository_profile: ado-maven

repositories:
  ado-maven:
    provider: ado
    organization: contoso
    project: platform
    feed: approved-packages
    credential_ref: ADO_ARTIFACT_PAT

options:
  existing_package_policy: SKIP_IDENTICAL
  timeout: 5m
  retry_count: 2
  dry_run: false
  parallelism: 4
  fail_fast: false
```

批次發佈至 Nexus 時只需替換 repository 區塊：

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

每個可發佈版本目錄只能有一個 POM；若沒有 POM，則必須恰好有一個非 sources、javadoc、tests 的主 JAR。

### 7.2 單一 JAR

若 JAR 含 `META-INF/maven/**/pom.properties`，工具會讀取 GAV、驗證檔名並產生最小 POM：

```yaml
package:
  path: ./artifacts/demo-1.2.3.jar
  format: maven
  publish_driver: maven_cli
  recursive: false
```

若 JAR 沒有內嵌 Maven metadata，必須同時提供三個 fallback 欄位：

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

固定 fallback GAV 不適合多個 JAR 的 recursive batch，否則不同檔案會共用同一組座標。

## 8. npm 發佈

支援以下輸入：

- `npm pack` 產生的 `.tgz`，內容必須包含 `package/package.json`。
- 含 `package.json` 的 package directory。
- `npm install` 產生的 `node_modules`。
- Scoped package，例如 `@company/demo`。

批次發佈至 ADO：

```yaml
package:
  path: ./downloaded-npm-repository
  format: npm
  publish_driver: npm_cli
  recursive: true
  npm:
    tag: legacy

repository_profile: ado-npm

repositories:
  ado-npm:
    provider: ado
    organization: contoso
    project: platform
    feed: approved-packages
    credential_ref: ADO_ARTIFACT_PAT

options:
  existing_package_policy: SKIP_IDENTICAL
  timeout: 5m
  retry_count: 2
  dry_run: false
  parallelism: 8
  fail_fast: false
```

發佈至 Nexus：

```yaml
repository_profile: nexus-npm

repositories:
  nexus-npm:
    provider: nexus
    base_url: https://nexus.example.com
    repository: npm-hosted
    username: publisher
    credential_ref: NEXUS_PASSWORD
```

`npm.tag` 為選填。未設定時 npm 使用預設 `latest`；回補舊版時可使用 `legacy`、`v4` 等非 `latest` tag：

```yaml
package:
  path: ./demo-2.0.0.tgz
  format: npm
  publish_driver: npm_cli
  recursive: false
  npm:
    tag: legacy
```

注意事項：

- `private: true` 的套件會被拒絕。
- 輸入是 package directory 時，工具會執行 `npm pack <directory> --json --ignore-scripts`。
- 發佈使用 `--ignore-scripts` 與 `--provenance=false`。
- Recursive 探索會辨識 `node_modules` 真正的套件根目錄，並依 name/version 去重。

## 9. PyPI 發佈

支援 wheel（`.whl`）及 source distribution（`.tar.gz`、`.zip`）。套件名稱及版本會從 `METADATA` 或 `PKG-INFO` 讀取。

可先下載待核准套件：

```bash
mkdir -p downloaded-pypi-repository
python3 -m pip download --dest downloaded-pypi-repository requests flask
```

批次發佈至 ADO：

```yaml
package:
  path: ./downloaded-pypi-repository
  format: pypi
  publish_driver: twine
  recursive: true

repository_profile: ado-pypi

repositories:
  ado-pypi:
    provider: ado
    organization: contoso
    project: platform
    feed: approved-packages
    credential_ref: ADO_ARTIFACT_PAT

options:
  existing_package_policy: SKIP_IDENTICAL
  timeout: 5m
  retry_count: 2
  dry_run: false
  parallelism: 4
  fail_fast: false
```

發佈至 Nexus：

```yaml
repository_profile: nexus-pypi

repositories:
  nexus-pypi:
    provider: nexus
    base_url: https://nexus.example.com
    repository: pypi-hosted
    username: publisher
    credential_ref: NEXUS_PASSWORD
```

同一目錄內相同 name/version 的 wheel 與 sdist 會合併為一個 bundle，再由 Twine 一次上傳。Recursive discovery 會依正規化後的 name/version 去重。

## 10. 外層封裝壓縮檔

如果掃描或核准系統將多個套件包成一個壓縮檔，使用 `archive_path`：

```yaml
package:
  archive_path: ./approved-packages.zip
  format: maven
  publish_driver: maven_cli
  recursive: true
```

支援 `.zip`、`.tar`、`.tar.gz`、`.tgz`。工具會解壓至暫存目錄、執行探索及發佈，結束後清除暫存內容。

`archive_path` 是包住套件的外層封裝。若 npm `.tgz` 或 PyPI `.zip` 本身就是要發佈的套件，仍應使用 `path`：

```yaml
package:
  path: ./demo-1.0.0.tgz
  format: npm
  publish_driver: npm_cli
  recursive: false
```

為避免路徑穿越，壓縮檔中的絕對路徑、上層路徑、符號連結及特殊檔案會被拒絕。

## 11. 發佈前驗證

### 11.1 本機模擬

將 `repository_profile` 設為 `test`，可在不設定 repository、不提供 credential、也不執行外部上傳工具的情況下驗證本機套件：

```yaml
package:
  path: ./downloaded-npm-repository
  format: npm
  publish_driver: npm_cli
  recursive: true

repository_profile: test

options:
  parallelism: 4
  fail_fast: false
```

模擬模式只驗證探索、套件解析與本機內容；不代表遠端連線及權限正確。

### 11.2 Dry run

要同時驗證 credential、repository 連線及遠端版本狀態，使用：

```yaml
options:
  existing_package_policy: SKIP_IDENTICAL
  timeout: 5m
  retry_count: 0
  dry_run: true
  parallelism: 4
  fail_fast: false
```

建議上線前依序執行：

1. `repository_profile: test` 驗證本機套件。
2. 切回正式 profile，設定 `dry_run: true` 驗證遠端。
3. 確認結果後設定 `dry_run: false` 正式發佈。

## 12. 批次模式

`recursive: true` 會啟用批次探索。即使套件很多，也只會同時執行 `parallelism` 指定數量的 worker。

```yaml
package:
  path: ./approved-packages
  format: pypi
  publish_driver: twine
  recursive: true

options:
  parallelism: 6
  fail_fast: false
```

- `fail_fast: false`：某個套件失敗後繼續處理其他套件。
- `fail_fast: true`：第一個錯誤後取消尚未開始的項目。
- 已成功發佈的套件不會因後續錯誤而 rollback。
- JSON report 的結果順序維持 discovery 順序。

大量發佈時，建議先使用較低的 `parallelism`（例如 4），再依 repository 負載與網路狀況調整。

## 13. 輸出、狀態與 Exit Code

單套件輸出 `PublishResult`，批次輸出 `BatchPublishReport`，格式皆為 JSON。

主要狀態：

| 狀態 | 意義 |
| --- | --- |
| `SUCCESS` | 發佈及遠端 checksum 驗證成功 |
| `SKIPPED` | 相同內容已存在，或本次為 dry-run |
| `FAILED` | 設定、套件、連線、衝突、發佈或驗證失敗 |

常見 `errorType`：

| 類型 | 說明 |
| --- | --- |
| `CONFIGURATION` | YAML、profile、credential 或 option 錯誤 |
| `PACKAGE` | 套件探索、metadata 或檔案結構錯誤 |
| `CONNECTION` | repository 連線或認證失敗 |
| `CONFLICT` | 同版本已存在且不符合 policy |
| `PUBLISH` | Maven、npm 或 Twine 上傳失敗 |
| `VERIFICATION` | 上傳後下載比對失敗 |
| `WORKER_INIT` | 批次 worker 初始化失敗 |
| `CANCELLED` | fail-fast 或 context 取消 |

Exit code：

| Code | 意義 |
| ---: | --- |
| `0` | 全部成功或安全跳過 |
| `1` | 套件處理、發佈或至少一個 batch item 失敗 |
| `2` | CLI 參數、YAML 設定或 credential 初始化失敗 |

Shell 中可檢查：

```bash
./package-publisher publish --config publisher.yaml
result=$?
echo "exit code: ${result}"
```

## 14. CI/CD 使用範例

設定檔可以提交到版本控制，但不可包含 PAT 或密碼：

```yaml
repository_profile: ado-production

repositories:
  ado-production:
    provider: ado
    organization: contoso
    project: platform
    feed: approved-packages
    credential_ref: ADO_ARTIFACT_PAT
```

Pipeline 執行環境應以 secret variable 提供 `ADO_ARTIFACT_PAT`，再執行：

```bash
go build -o package-publisher ./cmd/publisher
./package-publisher publish --config publisher.yaml > publish-result.json
```

建議將 `publish-result.json` 保存為 pipeline artifact，並透過 `metadata` 寫入 build ID、commit SHA 與 correlation ID。

## 15. 本機檔案副作用與安全性

工具可能在套件目錄建立：

- Maven JAR-only 的最小 `.pom`。
- npm package directory 經 `npm pack` 產生的 `.tgz`。
- Maven、npm、PyPI 待發佈檔案的 `.sha256` sidecar。

若來源目錄必須保持唯讀，請先複製到 promotion workspace。

安全原則：

- PAT 與密碼只能透過 `credential_ref` 指向的環境變數提供。
- 不要將 secret 寫進 YAML、Git、命令列參數或 JSON report。
- Maven 臨時 settings 與 npm 臨時 `.npmrc` 權限為 `0600`。
- Twine credential 由環境傳入。
- 發佈錯誤會遮蔽已知 secret。
- 同版本不同內容永遠拒絕覆蓋。

## 16. 常見問題排查

### 顯示 `repository_profile ... does not match`

確認 `repository_profile` 與 `repositories` 下方的 key 完全相同，並注意大小寫。

### 顯示 credential environment variable 未設定

確認 `credential_ref` 填的是環境變數名稱，並在同一個執行環境中設定：

```bash
export ADO_ARTIFACT_PAT='...'
test -n "${ADO_ARTIFACT_PAT}" && echo "credential is set"
```

請勿在共享終端或 CI log 印出實際 secret。

### ADO 回傳 authentication failed 或 permission denied

- 確認 PAT 尚未過期。
- 確認 PAT 具備 Packaging Read & Write。
- 確認使用者對目標 Feed 具有寫入權限。
- 確認 organization、project 與 feed 名稱正確。
- Organization-scoped Feed 應將 `project` 留空。

### Nexus 回傳 not found

- `base_url` 應為 server 根 URL，不可包含 `/repository/<name>`。
- `repository` 應填 hosted repository 名稱。
- 確認 repository 格式與 `package.format` 相容。
- 若 Nexus 使用 context path，確認 `base_url` 已包含該 path。

### 找不到 `mvn`、`npm` 或 Twine

確認工具已安裝於執行帳號的 `PATH`：

```bash
command -v mvn
command -v npm
python3 -m twine --version
```

### Maven 目錄包含多個 POM

每個版本目錄只能代表一個可獨立發佈的 bundle。請將不同 artifact 或版本拆到不同目錄。

### JAR-only 無法取得座標

檢查 JAR 是否包含 `META-INF/maven/**/pom.properties`；若沒有，於非 recursive 的單套件設定中提供完整 `maven.group_id`、`artifact_id` 與 `version`。

### npm 套件被拒絕

- 檢查 `package.json` 是否有合法 `name` 與 `version`。
- 檢查是否設定 `"private": true`。
- `.tgz` 必須是標準 npm pack 格式，且包含 `package/package.json`。

### PyPI 套件無法辨識

Wheel 必須包含 `.dist-info/METADATA`，sdist 必須包含 `PKG-INFO`；工具不會只依檔名猜測 name/version。

### 相同版本發佈失敗

工具會下載遠端內容並比對 bundle checksum。若遠端同版本內容不同，必須改用新版本，不能用 policy 強制覆蓋。

## 17. 完整設定檔範本

以下範本包含 ADO 與三種 Nexus repository。依需求調整 `package` 與 `repository_profile`：

```yaml
package:
  path: ./approved-packages
  format: maven
  publish_driver: maven_cli
  recursive: true

repository_profile: ado-production

repositories:
  ado-production:
    provider: ado
    organization: contoso
    project: platform
    feed: approved-packages
    credential_ref: ADO_ARTIFACT_PAT

  nexus-maven:
    provider: nexus
    base_url: https://nexus.example.com
    repository: maven-hosted
    username: publisher
    credential_ref: NEXUS_PASSWORD

  nexus-npm:
    provider: nexus
    base_url: https://nexus.example.com
    repository: npm-hosted
    username: publisher
    credential_ref: NEXUS_PASSWORD

  nexus-pypi:
    provider: nexus
    base_url: https://nexus.example.com
    repository: pypi-hosted
    username: publisher
    credential_ref: NEXUS_PASSWORD

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

切換套件類型時，`format` 與 `publish_driver` 必須成對修改：

```yaml
# Maven
format: maven
publish_driver: maven_cli

# npm
format: npm
publish_driver: npm_cli

# PyPI
format: pypi
publish_driver: twine
```
