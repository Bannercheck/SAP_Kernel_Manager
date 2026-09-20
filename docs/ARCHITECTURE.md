# skm — SAP Kernel Manager · Mimari (v0.1 taslak)

Durum: **taslak, onay bekliyor**. Kararlar §2'de; itiraz gelmezse varsayılan olarak uygulanır.
İlerleme ve adım listesi `STATE.md`'de tutulur; bu dosya sadece tasarımı anlatır.

## 1. Amaç ve kapsam

**Amaç:** SAP NetWeaver / S/4HANA sistemlerinin kernel güncellemesini (SAPEXE + SAPEXEDB, opsiyonel IGS ve
SAP Host Agent) güvenli, tekrarlanabilir, geri alınabilir ve platform bağımsız şekilde yapan tek bir CLI aracı: `skm`.

**Hedef platformlar:** Linux x86_64 / ppc64le (SLES, RHEL) · AIX 7.x (ppc64) · Windows Server x64.
Solaris ve HP-UX güncel kernel'lerce desteklenmediği için kapsam dışıdır; platform katmanı ileride eklemeye izin verir.

**v1 kapsamı (tek host: sapmnt'nin bulunduğu global host üzerinde çalışır):**
- Keşif: host'taki SAP sistemleri, instance'lar, mevcut kernel sürümü
- Paket deposu: SAR dosyalarını içe alma, doğrulama, listeleme (çevrimdışı)
- Plan / ön kontrol: uyumluluk, disk, yetki, kilit
- Uygulama: stage → yedek → durdur → dağıt → düzelt → başlat → doğrula (checkpoint'li, kesintiden devam edebilen)
- Geri alma (rollback) ve çalıştırma geçmişi

**v1 kapsam dışı (sonraki fazlar):** SAP Support Portal'dan indirme (S-user), çoklu host orkestrasyonu,
DB client güncellemesi, SUM tarzı stack yükseltme, web arayüzü.

## 2. Temel kararlar

| # | Karar | Gerekçe |
|---|-------|---------|
| D1 | **Dil: Go**, `CGO_ENABLED=0`, tek statik binary | Hedef hostlara runtime kurulamaz. Go `linux/amd64`, `linux/ppc64le`, `aix/ppc64`, `windows/amd64` hepsini tek makineden cross-compile eder. Python AIX'te garanti değil, Java ağır. |
| D2 | SAP araçları **yeniden yazılmaz, çağrılır** | `SAPCAR`, `sapcontrol`, `saphostctrl`, `disp+work`, `sapcpe`, `saproot.sh`. SAPCAR formatı kapalıdır; `sapcontrol` tüm platformlarda aynı API'dir. |
| D3 | **Minimum bağımlılık** | stdlib + `gopkg.in/yaml.v3` + `golang.org/x/{sys,term}`. CLI için stdlib `flag` + küçük alt-komut yönlendiricisi. |
| D4 | Her adım **idempotent + journal'lı** | Kesinti sonrası `skm resume` kaldığı yerden devam eder. |
| D5 | **Yedeksiz asla üstüne yazılmaz** | Rollback her zaman mümkün olmalı. |
| D6 | **Önce plan, sonra uygula** | `skm plan` bir plan dosyası üretir; `skm apply` onu uygular. `--dry-run` her yerde. |
| D7 | **Yetki modeli:** `<sid>adm` olarak çalış, root gereken adımlar için `sudo` | `saproot.sh` ve AIX `slibclean` root ister. Windows'ta yerel admin yetkili `<SID>adm`. |
| D8 | **Kilit paylaşımlı dizinde** | `DIR_CT_RUN/../.skm.lock` → aynı SID'i iki host aynı anda güncelleyemez (sapmnt NFS paylaşımlı). |
| D9 | **Yedek, sistem durduktan sonra** alınır (kullanıcı prosedürü) | Stage sistem çalışırken yapılır; durdur → `WaitforStopped` → `exe_<tarih>` kopyası → dağıt. Kopya `<sid>adm:sapsys` sahipliğiyle, izinler korunarak. |

## 3. Katmanlı mimari

```
┌──────────────────────────────────────────────────────────────┐
│ cmd/skm            CLI: status · repo · plan · apply · resume │
│                         rollback · history · doctor · version │
├──────────────────────────────────────────────────────────────┤
│ internal/workflow  plan, adım motoru (Check/Do/Undo), journal │
├────────────────┬───────────────┬─────────────────────────────┤
│ internal/sap   │ internal/repo │ internal/config · lock · log│
│  discovery     │  SAR deposu   │                             │
│  sapcontrol    │  paket adları │                             │
│  kernel (ver.) │  sapcar       │                             │
├────────────────┴───────────────┴─────────────────────────────┤
│ internal/platform   OS soyutlaması (build tag'li)             │
│   linux · aix · windows                                       │
├──────────────────────────────────────────────────────────────┤
│ internal/exec       Runner arayüzü (gerçek / sahte)           │
└──────────────────────────────────────────────────────────────┘
                              │ çağırır
                              ▼
   sapcontrol · saphostctrl · SAPCAR · disp+work · sapcpe · saproot.sh · slibclean
```

Bağımlılık yönü yukarıdan aşağıya tektir. `platform` ve `exec` dışında hiçbir paket OS'a özel kod içermez.

## 4. Dizin yapısı

```
cmd/skm/main.go              giriş noktası, alt-komut yönlendirme
internal/cli/                komutlar, çıktı biçimleme (table/json), TTY/renk
internal/config/             skm.yaml yükleme, varsayılanlar, doğrulama
internal/exec/               Runner arayüzü, RealRunner, FakeRunner (test)
internal/platform/           Platform arayüzü; platform_linux.go, platform_aix.go, platform_windows.go
internal/sap/discovery/      saphostctrl ListInstances, /usr/sap/sapservices, Windows servisleri
internal/sap/sapcontrol/     sapcontrol sarmalayıcı + çıktı parser'ları
internal/sap/kernel/         disp+work -V parser, paket adı parser, uyumluluk kuralları
internal/sap/profile/        instance profil okuma (DIR_CT_RUN, Start_Program_xx, sapcpe satırları)
internal/repo/               yerel paket deposu (index.json, sha256), SAPCAR sarmalayıcı
internal/workflow/           Plan, Run, Journal, Step motoru, rollback
internal/workflow/steps/     preflight, stage, backup, stop, predeploy, deploy, postfix, start, verify, cleanup
internal/lock/               SID kilidi (paylaşımlı dizin)
internal/logging/            slog tabanlı; konsol + dosya
docs/                        bu dosya, kullanıcı dokümanları
testdata/                    golden çıktılar (disp+work -V, sapcontrol, saphostctrl; OS başına)
Makefile                     build · check · cross · release
```

## 5. Bileşenler

### 5.1 `platform` — OS soyutlaması

```go
type Platform interface {
    Name() string                            // "linux" | "aix" | "windows"
    KernelDirName() string                   // bkz. tablo
    ExeSuffix() string                       // "" | ".exe"
    LibPathVar() string                      // LD_LIBRARY_PATH | LIBPATH | PATH
    SIDAdmUser(sid string) string            // "abcadm" | "ABCadm"
    IsPrivileged() bool                      // root / yerel admin
    PreDeploy(ctx, *Target) error            // AIX: slibclean; Unix: cleanipc; saposcol -k (exe'den çalışıyorsa)
    PostDeploy(ctx, *Target) error           // Unix: saproot.sh <SID>; Windows: yok (ACL miras)
}
```

| Platform | Kernel dizin adı | Kernel yolu (DIR_CT_RUN) | Lib değişkeni | Root gereken |
|----------|------------------|--------------------------|---------------|--------------|
| Linux x86_64 | `linuxx86_64` | `/usr/sap/<SID>/SYS/exe/uc/linuxx86_64` → `/sapmnt/<SID>/exe/uc/…` | `LD_LIBRARY_PATH` | `saproot.sh` |
| Linux ppc64le | `linuxppc64le` | aynı şema | `LD_LIBRARY_PATH` | `saproot.sh` |
| AIX | `rs6000_64` | aynı şema | `LIBPATH` | `saproot.sh`, `slibclean` |
| Windows x64 | `NTAMD64` | `<drive>:\usr\sap\<SID>\SYS\exe\uc\NTAMD64` (paylaşım: `\\<host>\sapmnt\<SID>\SYS\exe\uc\NTAMD64`) | `PATH` | — (yerel admin) |

Kernel yolu asla tahmin edilmez: `sapcontrol -nr <NR> -function ParameterValue DIR_CT_RUN` ile okunur.

### 5.2 `exec.Runner`

```go
type Cmd struct { Path string; Args []string; Env []string; Dir string; RunAs string; Timeout time.Duration }
type Result struct { Stdout, Stderr string; ExitCode int; Duration time.Duration }
type Runner interface { Run(ctx context.Context, c Cmd) (Result, error) }
```
`RunAs` doluysa Unix'te `sudo -n -u <user>` (yapılandırılabilir), Windows'ta desteklenmez (hata).
`FakeRunner` testlerde komut → hazır cevap eşlemesi yapar; tüm iş mantığı bu sayede OS'suz test edilir.

### 5.3 `sap/discovery`

Öncelik sırası: (1) `saphostctrl -function ListInstances` (tüm platformlar, SAP Host Agent),
(2) Unix `/usr/sap/sapservices`, (3) Windows `SAP<SID>_<NR>` servisleri.

```go
type System struct { SID string; Instances []Instance; DirCtRun string; Kernel kernel.Version }
type Instance struct { Nr string; Host string; Type string /* ASCS,D,J,SCS,ERS,… */; Profile string; DirExecutable string }
```
`GetSystemInstanceList` ile diğer hostlardaki instance'lar da listelenir; v1'de uzak instance varsa
kullanıcı uyarılır ve `--local-only` istenir (kernel paylaşımlı olduğu için diğer hostlar da durdurulmalı).

### 5.4 `sap/sapcontrol`

Sarmalanan fonksiyonlar: `GetVersionInfo`, `GetProcessList`, `GetSystemInstanceList`, `ParameterValue`,
`StopSystem [ALL]`, `WaitforStopped`, `StartSystem [ALL]`, `WaitforStarted`, `StopService`, `StartService <SID>`,
`RestartService`. Her çıktı için parser + golden test. Exit kodları (`0/1/2/3/4`) ayrı hatalara çevrilir.

### 5.5 `sap/kernel`

- `Version{Release int; Patch int; Changelist int; Platform string; Unicode bool; DBClient string}` ← `disp+work -V` parser
- `Package{Component string /* SAPEXE, SAPEXEDB, IGSEXE, IGSHELPER, SAPHOSTAGENT */; Patch int; Number string; File string}`
  ← `SAPEXE_200-80007541.SAR` gibi adlar
- Uyumluluk kuralları (`Compat(current, target) []Finding`):
  - farklı release → **engel**, `--allow-release-change` ile geçilir
  - patch geriye → **engel**, `--allow-downgrade` ile geçilir
  - platform uyuşmazlığı (staged `disp+work -V` "compiled on" ≠ host) → **engel**, geçilemez
  - SAPEXE ve SAPEXEDB patch seviyeleri farklı → **uyarı**
  - SAPEXEDB varyantı (hdb/ora/db6/mss/syb) mevcut DB client ile uyuşmuyor → **engel**

Hedef sürüm yalnızca dosya adından değil, **staging dizinine açılıp `disp+work -V` çalıştırılarak** doğrulanır
(lib yolu değişkeni staging'e ayarlanır).

### 5.6 `repo` — paket deposu

Konum: `repo_dir` (varsayılan `~/.skm/repo`; paylaşımlı NFS önerilir). İçerik: SAR dosyaları + `index.json`
(`Package`, sha256, doğrulanmış `Version`, eklenme zamanı). `skm repo add` → `SAPCAR -tvf` ile bütünlük,
ad parse, staging'de `disp+work -V`, index'e yaz. `skm repo list/inspect/rm`.

### 5.7 `workflow` — adım motoru

```go
type Step interface {
    Name() string
    Check(ctx, *Run) (State, error)   // Done | Pending | Blocked  → idempotency
    Do(ctx, *Run) error
    Undo(ctx, *Run) error             // rollback; yoksa workflow.NoUndo
}
```
`Run` = plan + `runs_dir/<run-id>/{plan.json,state.json,journal.jsonl,logs/}`. Her `Do/Undo` öncesi/sonrası journal'a
satır yazılır; `skm resume <run-id>` `Check` ile tamamlanmışları atlar.

**Adım dizisi (ABAP, tek host):**

| # | Adım | Kesinti içinde? | Undo |
|---|------|-----------------|------|
| 1 | preflight: kilit al, `DIR_CT_RUN`, mevcut sürüm, instance listesi, yetki, disk ≥ 3× paket | hayır | kilidi bırak |
| 2 | stage: SAR'ları staging'e aç, `disp+work -V` ile hedefi doğrula, uyumluluk | hayır | staging sil |
| 3 | hook `pre_stop` | — | hook `undo_pre_stop` (ops.) |
| 4 | stop: `StopSystem ALL` → `WaitforStopped` (tüm instance'lar GRAY olana dek bekle) → her instance `StopService` | **evet** | start |
| 5 | backup: `DIR_CT_RUN` → aynı üst dizine `exe_<YYYYMMDD_HHMMSS>` kopyası, `<sid>adm:sapsys`, izinler korunur, manifest (sha256) | evet | — (yedek kalır) |
| 6 | predeploy (platform): AIX `slibclean`, Unix `cleanipc`, exe'den çalışan `saposcol -k` | evet | — |
| 7 | deploy: staging → `DIR_CT_RUN` üstüne kopyala (izinler korunur, eski fazla dosyalar **silinmez**) | evet | yedeği geri kopyala |
| 8 | postfix: Unix `saproot.sh <SID>` (sudo); her instance için profildeki `sapcpe` satırlarını çalıştır | evet | aynısı (yedek için) |
| 9 | start: `StartService` → `StartSystem ALL` → `WaitforStarted` | evet | stop |
| 10 | hook `post_start` | — | — |
| 11 | verify: `GetVersionInfo`/`disp+work -V` == hedef, `GetProcessList` tümü GREEN | hayır | — |
| 12 | cleanup: staging sil, yedek saklama politikası (`backup.keep`) | hayır | — |

Yedek adı ve konumu `backup.dir` / `backup.name` ile değiştirilebilir (varsayılan: `DIR_CT_RUN`'ın yanına `exe_<tarih>`).
Araç root olarak çalışıyorsa kopya `RunAs=<sid>adm` ile yapılır; `<sid>adm` olarak çalışıyorsa sahiplik doğal olarak `<sid>adm:sapsys` olur.

**Rollback** = 4 → 6 → yedeği `DIR_CT_RUN`'a geri kopyala → 8 → 9 → 11. Rollback da journal'lıdır ve resume edilebilir.

**Run durum makinesi:** `planned → running → succeeded | failed → (resume→running | rollback→rolling_back → rolled_back | rollback_failed)`.
`rollback_failed` çıkış kodu 6'dır ve manuel müdahale gerektiğini açıkça yazar.

### 5.8 `config` — `skm.yaml`

Arama sırası: `--config` → `$SKM_CONFIG` → `~/.skm/skm.yaml` → `/etc/skm/skm.yaml` (Windows: `%ProgramData%\skm\skm.yaml`).

```yaml
repo_dir: /sapmnt/skm/repo      # paylaşımlı önerilir
runs_dir: ~/.skm/runs
sudo: "sudo -n"                 # Unix; root gereken adımlar için
backup: { keep: 2, dir: "", name: "exe_{{ts}}" }   # dir boş → DIR_CT_RUN'ın yanına; ts = YYYYMMDD_HHMMSS
timeouts: { stop: 600s, start: 900s, command: 120s }
hooks:
  pre_stop:   ["/usr/local/bin/cluster-maint on ABC"]   # HA/cluster entegrasyonu buradan
  post_start: ["/usr/local/bin/cluster-maint off ABC"]
systems:
  ABC:
    db: hdb                     # SAPEXEDB varyantı: hdb | ora | db6 | mss | syb
    include: [SAPEXE, SAPEXEDB, IGSEXE, IGSHELPER]
```

### 5.9 CLI ve çıktı standardı

| Komut | İş |
|-------|----|
| `skm status [--sid ABC]` | **açılış ekranı**: SID · hostname · instance no · instance tipi · sistem durumu (GREEN/YELLOW/GRAY) · sapstartsrv çalışıyor mu · SAP Host Agent çalışıyor mu/sürümü · kernel release/patch |
| `skm repo add <sar…> / list / inspect <id> / rm <id>` | paket deposu |
| `skm plan --sid ABC --patch 200 [--release 793]` | plan dosyası üret, ön kontrolleri göster |
| `skm apply <plan-id> [--yes] [--dry-run]` | planı uygula |
| `skm resume <run-id>` / `skm rollback <run-id>` | devam / geri al |
| `skm history [--sid ABC]` | çalıştırma geçmişi |
| `skm doctor` | araç yolları, yetkiler, sapcontrol erişimi |
| `skm version` | sürüm, commit, hedef OS/arch |

Çıktı kuralları: `--output table|json` (json = makine, tablo = insan); renk yalnızca TTY'de, `NO_COLOR` saygı görür;
adım ilerlemesi tek satır: `[7/12] deploy  ABC  … ok (38s)`; geri dönülmez işlemler öncesi `--yes` yoksa onay istenir;
tüm loglar ayrıca `runs_dir/<run-id>/logs/` altına yazılır.

Çıkış kodları: `0` başarılı · `1` genel hata · `2` kullanım · `3` preflight engeli · `4` kilitli · `5` apply başarısız,
rollback tamam · `6` rollback başarısız (manuel) · `7` verify başarısız.

## 6. Platform notları

- **Linux:** standart; `saproot.sh` için `sudo -n` gerekir (`skm doctor` kontrol eder).
- **AIX:** kütüphaneler bellekte kalır → dağıtımdan önce root ile `slibclean`, ardından `genkld` ile hâlâ yüklü lib kontrolü.
  `LIBPATH` kullanılır. Go `aix/ppc64` portu CGO'suz derlenir.
- **Windows:** dosyalar servisler çalışırken kilitlidir → `StopService` şart; `RunAs` yok, aracın kendisi `<SID>adm`
  (yerel admin) ile çalıştırılır. Yollar UNC olabilir; kopyada ACL mirası yeterlidir. Windows'a özel: `saposcol` servisi.
- **Çoklu host (v2):** kernel dizini paylaşımlı olduğu için tek kopya yeter, ama tüm hostlardaki instance'lar
  durdurulup başlatılmalı. Tasarım: her host'ta `skm agent` veya SSH ile `skm step …` çağrıları; `Runner` arayüzü bunu
  uzak Runner ile karşılar. v1 uzak instance görürse durur.

## 7. Güvenlik

- Kimlik bilgisi (v2 indirme için S-user) asla `skm.yaml`'a yazılmaz: env değişkeni veya etkileşimli istem; loglarda maskeleme.
- İndirilen dosyaların SHA-256'sı SAP'nin verdiği değerle karşılaştırılır (v2).
- Root yetkisi yalnızca `PostDeploy/PreDeploy` adımlarında, yapılandırılmış `sudo` komutu ile; hangi komutların
  çalıştırılacağı `skm plan` çıktısında görünür.
- Hook'lar açıkça yapılandırılmadıkça çalışmaz; çalışma kullanıcısı ve exit kodu journal'a yazılır.

## 8. Test stratejisi

- Birim: tüm parser'lar golden dosyalarla (`testdata/<os>/…`), `FakeRunner` ile iş mantığı.
- Workflow: sahte SAP sistemi (PATH'e konan `sapcontrol`/`SAPCAR`/`disp+work` script'leri) ile Linux CI'da uçtan uca.
- Kesinti/resume: her adım sonrasında öldürülüp `resume` edilen senaryo testi.
- AIX/Windows: CI'da yalnızca cross-build; gerçek doğrulama kullanıcı hostlarında `skm doctor` + `--dry-run` ile.

## 9. Build & release

`Makefile`: `build`, `check` (build+vet+test), `cross` (4 hedef, `dist/skm-<os>-<arch>[.exe]` + `SHA256SUMS`),
`release` (tag → GitHub Release). Sürüm bilgisi `-ldflags -X` ile gömülür. Go ≥ 1.24, `go.mod`'da toolchain pinlenir.

## 10. Açık sorular (kullanıcıya)

1. Dil Go ile devam mı? (D1) Alternatif: Python (AIX'te interpreter riski) / Java (SAP JVM'e bağımlılık).
2. v1'de yalnızca ABAP mı, Java (SCS/J) instance'ları da mı? Varsayılan: ABAP + ASCS/ERS; Java v2.
3. Paket deposu paylaşımlı NFS'te mi tutulacak? Varsayılan: evet, `repo_dir` yapılandırılabilir.
4. Windows için hedef: sadece tek host (global host) mı? Varsayılan: evet.
5. Binary adı `skm` uygun mu?
