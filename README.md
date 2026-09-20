# KernelMan — SAP Kernel Manager

SAP NetWeaver / S/4HANA kernel güncellemelerini Linux, AIX ve Windows üzerinde güvenli, geri alınabilir ve
tekrarlanabilir şekilde yapan tek binary CLI aracı.

- Mimari: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- İlerleme / yol haritası: [`STATE.md`](STATE.md)
- Geliştirme kuralları: [`CLAUDE.md`](CLAUDE.md)

## Kurulum (hedef sunucuya hiçbir şey kurulmaz)

`make cross` çıktısı olan `dist/` klasörünü sunucuya kopyalayın:

```
dist/kernelman.sh                  Linux / AIX başlatıcı (doğru binary'yi seçer)
dist/kernelman.bat                 Windows başlatıcı
dist/bin/kernelman-linux-amd64     tek başına çalışan yerel binary'ler
dist/bin/kernelman-linux-ppc64le
dist/bin/kernelman-aix-ppc64
dist/bin/kernelman-windows-amd64.exe
```

```sh
./kernelman.sh status              # <sid>adm olarak çalıştırın
./kernelman.sh status --sid ABC --output json
```

Örnek ekran: [`docs/examples/status-linux.txt`](docs/examples/status-linux.txt)

## Geliştirme

```sh
make check    # gofmt + vet + test + build
make cross    # 4 platform için dist/ üret
```

Durum: Adım 1 (durum ekranı) tamam; sıradaki adım `STATE.md`'de.
