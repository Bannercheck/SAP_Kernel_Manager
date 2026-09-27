# KernelMan — SAP Kernel Manager

SAP NetWeaver / S/4HANA kernel güncellemesini Linux ve AIX üzerinde menüden adım adım yapan tek binary konsol aracı:
**1** SAP Status · **2** Kernel Backup (her kernel dizini kendi yanına `<ad>_<tarih>`) · **3** Kernel Download (isteğe bağlı, internet ister; S-user ile SAP Software Center'da bu kernel'in
arşivlerini bulur, onayınızla `/usr/sap/download`'a indirir; MFA kodu terminalden sorulur) · **4** Kernel File Transfer (tüm sunucuda bugün tarihli SAR'ları bulur) · **5** SAP Stop/Start ·
**6** Kernel Update (tüm kernel dizinlerinde patch sırasıyla `SAPCAR -xvf`, chown, saproot.sh, doğrulama) · **7** Kernel Rollback.
Sorular tuşla cevaplanır: `Y`/`N`, `S`/`K`, `M` ana menü.

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
./kernelman.sh download                                      # S-user sorulur, arşivler bulunur, Y ile indirilir
./kernelman.sh download --basket ~/DownloadBasket.txt        # yedek yol: Download Basket dışa aktarımı
./kernelman.sh files --yes && ./kernelman.sh update --yes --start   # --from DIR ile tarama tek dizine daraltılır
```

`<sid>adm` ile çalıştırmak yeterlidir (dosyalar zaten `<sid>adm:sapsys` olur). Root ile çalıştırılırsa dosya işlemleri ve sapcontrol
`su - <sid>adm` ile yapılır ve `chown -R`, `saproot.sh` otomatik çalışır; `<sid>adm` ile çalışırken `saproot.sh` için ekranda root komutu gösterilir.
Ana menü `/usr/sap` ve `/sapmnt` kullanımını ve boş alanı gösterir; Kernel Backup önce yeterli yer olduğunu kontrol eder.

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
