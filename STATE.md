# STATE — skm ilerleme durumu

Son güncelleme: 2026-09-20 · Branch: `claude/great-turing-8e8v0c` · Faz: 0 (mimari)
Her oturum: bu dosyayı oku → sadece **NEXT** maddesini yap → burayı güncelle → commit+push.

## NEXT
- [ ] **Adım 1 — Temel + durum ekranı** (`skm status`) · bkz. `docs/ARCHITECTURE.md` §4, §5.1–5.5, §5.9
  - 1a Temel: `go.mod`, `cmd/skm`, `Makefile` (build/check/cross), `internal/exec` (Runner+Fake), `internal/logging`, `skm version`
  - 1b SAP katmanı: `internal/platform` (linux/aix/windows), `sap/discovery` (saphostctrl ListInstances + sapservices),
    `sap/sapcontrol` (GetProcessList, GetVersionInfo, ParameterValue, GetSystemInstanceList), `sap/kernel` (`disp+work -V` parser)
  - 1c Ekran: SID · hostname · instance no/tipi · sistem durumu · sapstartsrv çalışıyor mu · Host Agent (`saphostexec -status/-version`) · kernel release/patch
  - Golden testler: `testdata/linux/…` (AIX/Windows örnek çıktıları kullanıcıdan istenecek)

## Yol haritası
- [x] Adım 0 — Mimari, kurallar, bu dosya (`docs/ARCHITECTURE.md`, `CLAUDE.md`, `STATE.md`)
- [ ] Adım 1 — Temel + durum ekranı (yukarıda)
- [ ] Adım 2 — Durdur + yedekle: kilit → `StopSystem ALL` → `WaitforStopped` → `StopService` → `DIR_CT_RUN` → `exe_<YYYYMMDD_HHMMSS>` kopyası
      (`<sid>adm:sapsys`, izinler korunur, manifest) → journal; `skm stop/backup/start` komutları · §5.7 adım 4–5
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
- 2026-09-20 · Go seçildi; CGO kapalı; bağımlılık: yaml.v3, x/sys, x/term (gerekçe §2 D1–D3).
- 2026-09-20 · Kullanıcı prosedürü gereği yedek **durdurmadan sonra** alınır; ad `exe_<tarih>`, sahip `<sid>adm:sapsys` (D9).
- 2026-09-20 · Repo boştu; "design system çıkar" isteği uygulanamaz. CLI çıktı standardı §5.9'da tanımlandı; web UI gelirse gerçek design system yapılır.

## Notlar / engeller
- Uzak repoda henüz `main` yok; ilk push bu branch'ten. Kullanıcı isterse `main` bu branch'ten açılır.
- AIX ve Windows'ta gerçek `sapcontrol`/`saphostctrl`/`disp+work -V` çıktı örnekleri lazım (golden test için) → kullanıcıdan istenecek.
