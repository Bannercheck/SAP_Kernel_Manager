# STATE — kernelman ilerleme durumu

Son güncelleme: 2026-09-27 · Branch: `claude/great-turing-8e8v0c` · Faz: konsol akışı v1 (1–6) yazıldı → gerçek hostta doğrulama
Her oturum: bu dosyayı oku → sadece **NEXT** maddesini yap → burayı güncelle → commit+push.

## NEXT
- [ ] **Gerçek hostta doğrulama.** Kullanıcı Linux/AIX SAP hostunda `./kernelman.sh` çalıştırıp (root veya `<sid>adm`) menüden 1 → 2 → 3 → 4(K) → 5 → 4(S)
      akışını dener; çıktıları ve hataları gönderir. Golden test verisi olarak gerçek `sapcontrol`, `saphostctrl`, `disp+work -V`, `SAPCAR -xvf` çıktıları eklenir.
- [ ] Sonrası (kullanıcı geri bildirimine göre): AIX `slibclean` (root, dağıtımdan önce), `sapcpe` çalıştırma, journal/resume, Windows (kapsam dışı, istenirse).

## Yol haritası (2026-09-27 · kullanıcının 5 adımlık konsol akışı)
- [x] Adım 0 — Mimari, kurallar, bu dosya
- [x] Adım 1 — **SAP Status**: kernel sürümü, sapstartsrv/sapcontrol ışıkları, SID/hostname/instance otomatik; menü başlığında sistem + instance ışıkları
- [x] Adım 2 — **Kernel Backup**: `DIR_CT_RUN` → yanına `exe_<YYYYMMDD>` (`cp -pR`, root ise `su - <sid>adm`), dosya sayısı doğrulanır, `ls -la` listesi ekranda
- [x] Adım 3 — **Kernel Files**: indirme dizini sorulur; **yalnızca bugün tarihli** `*.SAR/*.sar` dosyaları kernel dizinine kopyalanır; `chown -R <sid>adm:sapsys`; liste
- [x] Adım 4 — **SAP Stop / Start**: `K` = StopSystem ALL → WaitforStopped → StopService; `S` = StartService (sidadm) → StartSystem ALL → WaitforStarted; ışıklar
- [x] Adım 5 — **Kernel Update**: sistem durmuş + bugünkü yedek şart (yoksa teklif eder); arşivler **patch numarasına göre küçükten büyüğe** `SAPCAR -xvf`
      (SAPEXE → SAPEXEDB → tek bileşen yamaları); `chown -R`; `saproot.sh` (root); `disp+work -V` ile yeni seviye; `S` ile başlat
- [x] Adım 6 — **Kernel Rollback**: en son `exe_<date>` yedeğini kernel dizininin üstüne kopyalar, chown, doğrula, başlat teklifi
- [x] Her işlem sonunda ✔/✘ + "Press Enter to return to the main menu" → ana sayfa ışıklarla yenilenir
- [ ] Sonraki: gerçek host doğrulaması (NEXT), AIX `slibclean`, journal/resume, çoklu host

## Açık kararlar (kullanıcı onayı bekliyor — itiraz yoksa varsayılan uygulanır)
- Dil **Go** (D1). Alternatifler: Python (AIX riski), Java (SAP JVM bağımlılığı).
- v1 yalnızca ABAP (+ASCS/ERS); Java instance'ları v2.
- Paket deposu paylaşımlı NFS'te (`repo_dir`).
- Windows: yalnızca tek host (global host).
- Binary adı `kernelman`.

## Karar günlüğü
- 2026-09-27 · Kullanıcı (4. tur): **disk bölümü** — ana menüde `/usr/sap` ve `/sapmnt` için kullanılan alan (en büyük 3 alt dizinle) + boş alan ışığı
  (yeşil >%20, sarı >%10, kırmızı); SAP Status'ta `df -Pk` tablosu + `du -sk` dizin boyutları (en büyük 20 + toplam). Kernel Backup öncesi
  "Check free space" adımı (mount başına gereken + %10 pay). `internal/disk` paketi (Linux/AIX uyumlu `df -Pk`, `du -sk`).
- 2026-09-27 · Tablo başlıkları (STATE, SYSTEM, TYPE, HOSTNAME …) kalın-cyan başlık stili; değerlerden ayrışır.
- 2026-09-27 · **root şart değil:** `<sid>adm` ile tüm işlemler çalışır (dosyalar zaten `<sid>adm:sapsys` olur, chown "not needed");
  root ise `su - <sid>adm`; başka kullanıcı ise uyarı + sonradan chown talimatı. Yalnızca `saproot.sh` root ister; ekranda komut verilir.
  Ekranda "running as …" satırı.
- 2026-09-27 · Kullanıcı (3. tur): tarama **tüm sunucu** (`/` kökü; /proc /sys /dev /usr/lib /usr/share /var/lib /hana/data … ve kernel dizinleri + yedekler atlanır,
  ilerleme sayacı gösterilir). Kernel Update de snapshot kopyalarına değil sunucu taramasına bakar; FOUND IN gerçek indirme yeri.
- 2026-09-27 · İstem tasarımı: numaralar yalnızca ana menüde. Evet/hayır `[Y] Yes [N] No`, durdur/başlat `[S] Start SAP [K] Stop SAP`, işlem sonunda
  `[M] Main menu [Q] Quit`; tuşa basınca geçer, Enter tek başına seçim yapmaz. "Kernel Files" → **"Kernel File Transfer"**.
- 2026-09-27 · Kullanıcı düzeltmeleri (2. tur): **bütün kernel dizinleri** (merkezi DIR_CT_RUN + her instance'ın DIR_EXECUTABLE) yedeklenir,
  SAR'lar hepsine kopyalanır, hepsinde sırayla açılır, hepsi chown edilir. Yedek adı **kendi adı + tarih, kendi yanında**:
  `.../D00/exe` → `.../D00/exe_20260927`, `.../uc/linuxx86_64` → `.../uc/linuxx86_64_20260927`. Tam `ls -la` listesi `<state>/backup_<ts>.log`.
- 2026-09-27 · İndirme dizini sorulmaz: **sunucu taranır** (`internal/ops/scan.go`; kökler: cwd, /usr/sap, /sapmnt, /tmp, /home, /root, /mnt, /opt …;
  `KERNELMAN_SCAN_ROOTS` ile değiştirilebilir, `--from` tek dizin). Bugün tarihli `*.SAR/*.sar`; kernel dizinleri ve yedekleri hariç; aynı ad → en yenisi.
- 2026-09-27 · Enter ile bitirme/iptal kaldırıldı: her istem menü gibi numaralı (`1) Yes 0) No`, `1) SAP Start 2) SAP Stop 0) Back`,
  işlem sonunda `0) Back to main menu q) Quit`); Enter tek başına hiçbir şey seçmez. Değer istemlerinde (yedek yolu) Enter = varsayılan.
- 2026-09-27 · Görünüm: sistem tipi "AS ABAP / AS Java / Dual-stack (AS ABAP + AS Java)"; başlıkta Hostname · User · tarih; etiketler Title Case;
  "Kernel 793 Patch 200"; bileşen adı dosyadaki haliyle (dw, igsexe); sistem durumu büyük rozet `⬤ RUNNING / PARTIAL / STOPPED` (kalın renkli).
- 2026-09-27 · **Demo modu** (`kernelman demo`, `--demo`, `KERNELMAN_DEMO=1`): `~/.kernelman/demo` altında simüle SAP hostu (`internal/demo`);
  cp/ls/chown gerçek, sapcontrol/SAPCAR/disp+work durum makinesiyle simüle. Kullanıcı macOS'ta tüm akışı deneyebilsin diye.
  `make cross` darwin/arm64 + darwin/amd64 de üretir (yalnızca demo için); `make macos` → `dist/kernelman-macos-demo.zip`.
- 2026-09-27 · Kullanıcı: **yalnızca Unix** (Linux, AIX); Windows kodu ve hedefi kaldırıldı (`platform_windows.go`, `vt_windows.go`, `.bat`). `make cross` → linux/amd64, linux/ppc64le, aix/ppc64.
- 2026-09-27 · Kullanıcı prosedürü: klasik yerinde güncelleme. SAR'lar kernel dizinine kopyalanır ve orada `SAPCAR -xvf` ile sırayla açılır (staging yok).
  Güvenlik: Kernel Update, sistem durmadan ve **bugünkü** yedek olmadan çalışmaz (yoksa önce alır). Rollback = yedeği geri kopyala.
- 2026-09-27 · Root'tan çalıştırma: dosya işlemleri ve sapcontrol Start/Stop `su - <sid>adm -c` ile; root olmayan aynı kullanıcı doğrudan; başka kullanıcı `sudo -n -u`.
  `chown -R` ve `saproot.sh` yalnızca root'ta çalışır, aksi halde ekranda "skipped" notu.
- 2026-09-27 · Bugün tarihli SAR filtresi: dosya mtime'ının günü = çalıştırma günü; eski arşivler sayı olarak gösterilir, kopyalanmaz.
- 2026-09-27 · Durum dizini `/usr/sap/<SID>/.kernelman/snapshot.json` (yazılamazsa `~/.kernelman/<SID>`): kernel dizini, son yedek, indirme dizini, kopyalanan SAR'lar.
  sapstartsrv kapalıyken kernel dizini: snapshot → `/usr/sap/<SID>/SYS/exe/run` symlink.
- 2026-09-27 · Menü: 6 madde (Status, Backup, Files, Stop/Start, Update, Rollback); `stop/start/version` yalnızca komut satırında. Update Plan/History/Doctor kaldırıldı.
- 2026-09-20 · Kullanıcı: menü kalabalık, kapalı sistem için yalnızca kırmızı ışık → açıklamalar menüden kaldırıldı (`help`'te), durum kelimeleri (running/stopped/n/m) kaldırıldı, ışık tek gösterge.
- 2026-09-20 · İsim **KernelMan** (kullanıcı seçimi): komut/binary `kernelman`, görünen ad `KernelMan`, env `KERNELMAN_*`.
- 2026-09-20 · Menü başlığı: sistem başına açık/kapalı ışığı + kernel + sapstartsrv n/m + her instance'ın ışığı + Host Agent (kullanıcı: "sistem açık mı kapalı mı görünsün").
- 2026-09-20 · İsim `skm` → **`kernelman`** (kullanıcı: kısaltma güzel değil). Tek yerden: `internal/version.AppName/ProductName/EnvPrefix`,
  Makefile `BIN`, `scripts/kernelman.{sh,bat}`, `cmd/kernelman`. Alternatifler sunuldu: `kernup`, `kernelctl`. Go modül yolu repo adı olarak kaldı.
- 2026-09-20 · Kullanıcı isteği: menüde yapılan işlemin yanında ✔/✘; durumlar trafik ışığı (GREEN yeşil, YELLOW sarı, RED+GRAY kırmızı).
  `internal/ui`: renk yalnızca TTY/`KERNELMAN_COLOR`, Unicode yalnızca UTF-8 locale (`KERNELMAN_UNICODE`), Windows VT modu syscall ile.
- 2026-09-20 · Kullanıcı: "Update Plan" menüde anlaşılmıyor → kaldırıldı; Kernel Update = dizin sor → plan göster → onay → uygula; `--dry-run` planda durur.
  "Kernel Packages" → "Kernel Files" (`files`), `apply` → `update`, `repo` kaldırıldı.
- 2026-09-20 · Kullanıcı: SAR'lar indirme dizininden alınır, kopyalamadan önce dizin sorulur (`--from`).
- 2026-09-20 · Kullanıcı (kritik): stack kernel (SAPEXE_400) önce, sonra tek bileşen yamaları artan sırayla (411, 412 … 420) → §5.5 kuralı.
- 2026-09-20 · Kullanıcı isteği: işlem adları anlaşılır olsun → `internal/cli/ops.go` tek kayıt (ID + görünen ad); argümansız `kernelman` terminalde menü açar.
- 2026-09-20 · sapstartsrv durduktan sonra `ParameterValue` çalışmaz → SAP Stop öncesi snapshot + `sappfpar` fallback (Adım 2/3).
- 2026-09-20 · Kullanıcı isteği: durdurma ve kopyalama ayrı adımlar/komutlar (`kernelman stop`, `kernelman backup`); `apply` bunları zincirler.
- 2026-09-20 · Her adımdan sonra `make examples` → `docs/examples/*.png` üretilir ve kullanıcıya gönderilir (kullanıcı macOS'ta, SAP hostu yok).
- 2026-09-20 · Adım 1 bitti. Dağıtım düzeni: `dist/kernelman.sh`, `dist/kernelman.bat`, `dist/bin/kernelman-<os>-<arch>`; hedef hosta hiçbir runtime
  kurulmaz (Go yalnızca build makinesinde). Testdata paket içinde (`internal/sap/*/testdata/linux`).
- 2026-09-20 · Gerçek SAP_BASIS sürümü OS seviyesinden okunamaz (DB/RFC gerekir); ekranda kernel'in desteklediği SVERS aralığı gösterilir.
- 2026-09-20 · sapcontrol `GetProcessList` çıkış kodu 3/4 başarı sayılır; `textstatus` virgül içerir → sağdan/soldan sabit sütun ayrıştırma.
- 2026-09-20 · Windows'ta `IsPrivileged` `\\.\PHYSICALDRIVE0` açma denemesiyle (x/sys bağımlılığı ertelendi).
- 2026-09-20 · Go seçildi; CGO kapalı; bağımlılık: yaml.v3, x/sys, x/term (gerekçe §2 D1–D3).
- 2026-09-20 · Kullanıcı prosedürü gereği yedek **durdurmadan sonra** alınır; ad `exe_<tarih>`, sahip `<sid>adm:sapsys` (D9).
- 2026-09-20 · (geçmiş) Repo boştu; "design system çıkar" isteği uygulanamaz. CLI çıktı standardı §5.9'da tanımlandı; web UI gelirse gerçek design system yapılır.

## Notlar / engeller
- Uzak repoda henüz `main` yok; ilk push bu branch'ten. Kullanıcı isterse `main` bu branch'ten açılır.
- AIX ve Windows'ta gerçek `sapcontrol`/`saphostctrl`/`disp+work -V` çıktı örnekleri lazım (golden test için) → kullanıcıdan istenecek.
- `kernelman status` gerçek bir SAP hostunda henüz denenmedi; ilk gerçek çalıştırma çıktısı (`--output json`) kullanıcıdan istenecek.
- Bağımlılık yok (stdlib only); `go.sum` yok. yaml.v3 Adım 4'te (config) gelecek.
