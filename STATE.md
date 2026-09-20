# STATE — skm ilerleme durumu

Son güncelleme: 2026-09-20 · Branch: `claude/great-turing-8e8v0c` · Faz: 1 tamam → Adım 2
Her oturum: bu dosyayı oku → sadece **NEXT** maddesini yap → burayı güncelle → commit+push.

## NEXT
- [ ] **Adım 2 — Durdur** (`skm stop`) · bkz. `docs/ARCHITECTURE.md` §5.7 adım 4, §5.2 (RunAs)
  - 2a `internal/lock`: `DIR_CT_RUN/../.skm.lock` (SID, host, pid, zaman; stale tespiti)
  - 2b `sap/sapcontrol`: `StopSystem [ALL]`, `WaitforStopped <timeout> <delay>`, `StopService`, `StartService <SID>`, `StartSystem`, `WaitforStarted`
  - 2c `internal/workflow` çekirdeği: `Step{Check,Do,Undo}`, `Run`, JSONL journal (`~/.skm/runs/<id>/`), `resume`
  - 2d **SAP Stop** adımı: snapshot yaz (`~/.skm/systems/<SID>.json`: DIR_CT_RUN, DIR_EXE_ROOT, instance'lar, profiller) →
    `StopSystem ALL` → `WaitforStopped` (tüm instance'lar GRAY olana dek) → her local instance `StopService`; Undo = **SAP Start**
  - 2f çevrimdışı parametre çözümü: `sappfpar pf=<profil> <param>` (sapstartsrv kapalıyken); Windows keşif: `sc qc SAP<SID>_<NR>` parser
  - Adım/işlem adları `internal/cli/ops.go` kaydından gelir (SAP Status, SAP Stop, SAP Start, Kernel Backup …); menü bu kaydı kullanır
  - 2e CLI: `skm stop --sid ABC [--yes] [--dry-run] [--timeout]`, `skm start --sid ABC`; ekranda adım ilerlemesi (`[1/3] StopSystem ... ok (42s)`)
  - Test: FakeRunner ile stop→wait senaryosu; `make examples` ile `docs/examples/stop-linux.png`

## Yol haritası
- [x] Adım 0 — Mimari, kurallar, bu dosya (`docs/ARCHITECTURE.md`, `CLAUDE.md`, `STATE.md`)
- [x] Adım 1 — Temel + durum ekranı: `go.mod`, `Makefile` (build/check/cross/examples), `internal/{exec,platform,version,cli}`,
      `internal/sap/{kernel,sapcontrol,discovery,status}`, `skm status/version`, golden testler, `dist/` + `skm.sh`/`skm.bat`.
      Ekranlar: `docs/examples/*.png` (menü dahil)
- [ ] Adım 2 — Durdur (NEXT, yukarıda)
- [ ] Adım 3 — Yedekle (`skm backup --sid ABC`): sistem durmuş olmalı (Check) → `DIR_CT_RUN` → `<üst dizin>/exe_<YYYYMMDD_HHMMSS>`
      kopyası; izin/sahiplik korunur (`<sid>adm:sapsys`; root ise `RunAs=<sid>adm`); manifest sha256; `backup.keep` · §5.7 adım 5
- [ ] Adım 4 — Paket deposu: SAR ad parser, SAPCAR sarmalayıcı, `skm repo add/list/inspect`, staging + `disp+work -V` doğrulama, uyumluluk · §5.5–5.6
- [ ] Adım 5 — `skm plan` + preflight (disk, yetki, kilit, uyumluluk, plan dosyası) · §5.7 adım 1–2
- [ ] Adım 6 — Workflow motoru tamamı: deploy, postfix (saproot.sh, sapcpe), start, verify, cleanup, `apply/resume/rollback/history` · §5.7
- [ ] Adım 7 — Windows sertleştirme (servisler, UNC yollar, kilitli dosyalar) · §6
- [ ] Adım 8 — AIX sertleştirme (`slibclean`, `genkld`, `LIBPATH`) · §6
- [ ] Adım 9 — İndirme: SAP Support Portal / S-user, SHA-256, `skm fetch` · §7
- [ ] Adım 10 — Ek bileşenler: IGS, SAP Host Agent (`saphostexec -upgrade`)
- [ ] Adım 11 — Çoklu host orkestrasyonu · §6
- [ ] Adım 12 — Release pipeline (GitHub Actions cross-build, SHA256SUMS) · §9

## Açık kararlar (kullanıcı onayı bekliyor — itiraz yoksa varsayılan uygulanır)
- Dil **Go** (D1). Alternatifler: Python (AIX riski), Java (SAP JVM bağımlılığı).
- v1 yalnızca ABAP (+ASCS/ERS); Java instance'ları v2.
- Paket deposu paylaşımlı NFS'te (`repo_dir`).
- Windows: yalnızca tek host (global host).
- Binary adı `skm`.

## Karar günlüğü
- 2026-09-20 · Kullanıcı isteği: işlem adları anlaşılır olsun → `internal/cli/ops.go` tek kayıt (ID + görünen ad); argümansız `skm` terminalde menü açar.
- 2026-09-20 · sapstartsrv durduktan sonra `ParameterValue` çalışmaz → SAP Stop öncesi snapshot + `sappfpar` fallback (Adım 2/3).
- 2026-09-20 · Kullanıcı isteği: durdurma ve kopyalama ayrı adımlar/komutlar (`skm stop`, `skm backup`); `apply` bunları zincirler.
- 2026-09-20 · Her adımdan sonra `make examples` → `docs/examples/*.png` üretilir ve kullanıcıya gönderilir (kullanıcı macOS'ta, SAP hostu yok).
- 2026-09-20 · Adım 1 bitti. Dağıtım düzeni: `dist/skm.sh`, `dist/skm.bat`, `dist/bin/skm-<os>-<arch>`; hedef hosta hiçbir runtime
  kurulmaz (Go yalnızca build makinesinde). Testdata paket içinde (`internal/sap/*/testdata/linux`).
- 2026-09-20 · Gerçek SAP_BASIS sürümü OS seviyesinden okunamaz (DB/RFC gerekir); ekranda kernel'in desteklediği SVERS aralığı gösterilir.
- 2026-09-20 · sapcontrol `GetProcessList` çıkış kodu 3/4 başarı sayılır; `textstatus` virgül içerir → sağdan/soldan sabit sütun ayrıştırma.
- 2026-09-20 · Windows'ta `IsPrivileged` `\\.\PHYSICALDRIVE0` açma denemesiyle (x/sys bağımlılığı ertelendi).
- 2026-09-20 · Go seçildi; CGO kapalı; bağımlılık: yaml.v3, x/sys, x/term (gerekçe §2 D1–D3).
- 2026-09-20 · Kullanıcı prosedürü gereği yedek **durdurmadan sonra** alınır; ad `exe_<tarih>`, sahip `<sid>adm:sapsys` (D9).
- 2026-09-20 · Repo boştu; "design system çıkar" isteği uygulanamaz. CLI çıktı standardı §5.9'da tanımlandı; web UI gelirse gerçek design system yapılır.

## Notlar / engeller
- Uzak repoda henüz `main` yok; ilk push bu branch'ten. Kullanıcı isterse `main` bu branch'ten açılır.
- AIX ve Windows'ta gerçek `sapcontrol`/`saphostctrl`/`disp+work -V` çıktı örnekleri lazım (golden test için) → kullanıcıdan istenecek.
- `skm status` gerçek bir SAP hostunda henüz denenmedi; ilk gerçek çalıştırma çıktısı (`--output json`) kullanıcıdan istenecek.
- Bağımlılık yok (stdlib only); `go.sum` yok. yaml.v3 Adım 4'te (config) gelecek.
