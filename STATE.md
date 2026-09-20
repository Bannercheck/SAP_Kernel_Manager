# STATE — skm ilerleme durumu

Son güncelleme: 2026-09-20 · Branch: `claude/great-turing-8e8v0c` · Faz: 1 tamam → Adım 2
Her oturum: bu dosyayı oku → sadece **NEXT** maddesini yap → burayı güncelle → commit+push.

## NEXT
- [ ] **Adım 2 — Durdur + yedekle** · bkz. `docs/ARCHITECTURE.md` §5.7 adım 4–5, §5.2 (RunAs)
  - 2a `internal/lock`: `DIR_CT_RUN/../.skm.lock` (SID, host, pid, zaman; stale tespiti)
  - 2b `sap/sapcontrol`: `StopSystem [ALL]`, `WaitforStopped <timeout> <delay>`, `StopService`, `StartService <SID>`, `StartSystem`, `WaitforStarted`, `RestartService`
  - 2c `internal/workflow` çekirdeği: `Step{Check,Do,Undo}`, `Run`, JSONL journal (`~/.skm/runs/<id>/`), `resume`
  - 2d adımlar: `stop` (StopSystem ALL → WaitforStopped → her local instance StopService) ve `backup`
    (`DIR_CT_RUN` → `<üst dizin>/exe_<YYYYMMDD_HHMMSS>`; izin/sahiplik korunur; root ise `RunAs=<sid>adm`; manifest sha256)
  - 2e CLI: `skm stop --sid`, `skm backup --sid`, `skm start --sid`, `skm resume <run-id>`; `--yes`, `--dry-run`
  - Test: FakeRunner ile stop→wait→backup senaryosu; kopya için temp dizinli gerçek dosya testi

## Yol haritası
- [x] Adım 0 — Mimari, kurallar, bu dosya (`docs/ARCHITECTURE.md`, `CLAUDE.md`, `STATE.md`)
- [x] Adım 1 — Temel + durum ekranı: `go.mod`, `Makefile` (build/check/cross), `internal/{exec,platform,version,cli}`,
      `internal/sap/{kernel,sapcontrol,discovery,status}`, `skm status/version`, golden testler, `dist/` + `skm.sh`/`skm.bat`.
      Örnek ekran: `docs/examples/status-linux.txt`
- [ ] Adım 2 — Durdur + yedekle (NEXT, yukarıda)
- [ ] Adım 3 — Paket deposu: SAR ad parser, SAPCAR sarmalayıcı, `skm repo add/list/inspect`, staging + `disp+work -V` doğrulama, uyumluluk · §5.5–5.6
- [ ] Adım 4 — `skm plan` + preflight (disk, yetki, kilit, uyumluluk, plan dosyası) · §5.7 adım 1–2
- [ ] Adım 5 — Workflow motoru tamamı: deploy, postfix (saproot.sh, sapcpe), start, verify, cleanup, `apply/resume/rollback/history` · §5.7
- [ ] Adım 6 — Windows sertleştirme (servisler, UNC yollar, kilitli dosyalar) · §6
- [ ] Adım 7 — AIX sertleştirme (`slibclean`, `genkld`, `LIBPATH`) · §6
- [ ] Adım 8 — İndirme: SAP Support Portal / S-user, SHA-256, `skm fetch` · §7
- [ ] Adım 9 — Ek bileşenler: IGS, SAP Host Agent (`saphostexec -upgrade`)
- [ ] Adım 10 — Çoklu host orkestrasyonu · §6
- [ ] Adım 11 — Release pipeline (GitHub Actions cross-build, SHA256SUMS) · §9

## Açık kararlar (kullanıcı onayı bekliyor — itiraz yoksa varsayılan uygulanır)
- Dil **Go** (D1). Alternatifler: Python (AIX riski), Java (SAP JVM bağımlılığı).
- v1 yalnızca ABAP (+ASCS/ERS); Java instance'ları v2.
- Paket deposu paylaşımlı NFS'te (`repo_dir`).
- Windows: yalnızca tek host (global host).
- Binary adı `skm`.

## Karar günlüğü
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
