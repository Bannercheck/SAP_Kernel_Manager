# KernelMan — SAP Kernel Manager

SAP NetWeaver / S/4HANA kernel güncellemesini Linux ve AIX üzerinde menüden adım adım yapan tek binary konsol aracı:
**1** SAP Status · **2** Kernel Backup (her kernel dizini kendi yanına `<ad>_<tarih>`) · **3** Kernel Files (sunucuda bugün tarihli SAR'ları bulur) ·
**4** SAP Stop/Start · **5** Kernel Update (tüm kernel dizinlerinde patch sırasıyla `SAPCAR -xvf`, chown, saproot.sh, doğrulama) · **6** Kernel Rollback.
Tüm sorular menü gibi numaralıdır (`1` evet, `0` geri).

- Mimari: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- İlerleme / yol haritası: [`STATE.md`](STATE.md)
- Geliştirme kuralları: [`CLAUDE.md`](CLAUDE.md)

## Kurulum (hedef sunucuya hiçbir şey kurulmaz)

`make cross` çıktısı olan `dist/` klasörünü sunucuya kopyalayın:

```
dist/kernelman.sh                  Linux / AIX başlatıcı (doğru binary'yi seçer)
dist/bin/kernelman-linux-amd64     tek başına çalışan yerel binary'ler
dist/bin/kernelman-linux-ppc64le
dist/bin/kernelman-aix-ppc64
```

```sh
cd /usr/sap && ./kernelman.sh      # root veya <sid>adm; argümansız = menü
./kernelman.sh status --output json
./kernelman.sh files --yes && ./kernelman.sh update --yes --start   # --from DIR ile tarama tek dizine daraltılır
```

Root olarak çalıştırıldığında dosya işlemleri ve sapcontrol `su - <sid>adm` ile yapılır; `chown -R <sid>adm:sapsys` ve
`saproot.sh` yalnızca root'ta çalışır.

Örnek ekran: [`docs/examples/status-linux.txt`](docs/examples/status-linux.txt)

## Demo (SAP olmayan makinede, macOS dahil)

```sh
./kernelman.sh demo        # ~/.kernelman/demo altında simüle SAP hostu; 6 işlemin hepsi denenebilir
```

Ayrıntı: [`docs/MACOS-DEMO.md`](docs/MACOS-DEMO.md)

## Geliştirme

```sh
make check    # gofmt + vet + test + build
make cross    # 4 platform için dist/ üret
```

Durum: 6 işlemlik konsol akışı yazıldı, gerçek SAP hostunda doğrulama bekliyor; ayrıntı `STATE.md`'de.
