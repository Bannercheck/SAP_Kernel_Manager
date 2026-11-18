# KernelMan Hub — hızlı başlangıç

Tek bir sunucudan (RHEL/SLES ya da kendi Windows PC'niz) bütün SAP sunucularının kernel güncellemesini
web panosundan yönetir. Hub, SAP hostlarına **SSH** ile bağlanır, KernelMan ajanını kopyalar ve işlemleri
(durum, yedek, dosya aktarımı, stop/start, kernel update, rollback) ajan üzerinden çalıştırır.
SAP hostlarına ayrıca bir şey kurulmaz; ajan ~10 MB'lık tek dosyadır.

## 1. RHEL sunucuda çalıştırma (deneme)

```sh
uname -m                                   # x86_64 → linux-amd64 · ppc64le → linux-ppc64le
unzip kernelman-hub-linux-amd64.zip        # veya: tar xzf kernelman-hub-linux-amd64.tar.gz
cd kernelman-hub-linux-amd64
chmod +x kernelman-hub                     # zip Mac'te açılıp tekrar kopyalandıysa çalıştırma izni gider
./kernelman-hub
```

`unzip` yoksa: `dnf install unzip` ya da `.tar.gz` paketini kullanın. Hub'ı root olmadan, kendi kullanıcınızla
çalıştırabilirsiniz (7443 ayrıcalıksız porttur); yalnızca güvenlik duvarı ve kalıcı servis için root gerekir.
Port başka bir uygulamadaysa (ör. 8443'te regista): `./kernelman-hub --port 9443` — hub dolu portu açıkça söyler.

Açılışta erişim linkleri yazılır (sunucu adı ve her IP için), örneğin:

```
  Dashboard  https://saphub01:7443
             https://10.20.30.15:7443
  Self-signed certificate: the browser warns once. Compare its SHA-256 fingerprint: …
  Firewall (RHEL): firewall-cmd --permanent --add-port=7443/tcp && firewall-cmd --reload
```

* Linux'ta varsayılan: **tüm arayüzlerde HTTPS 7443** (8443 başka iç uygulamalara bırakıldı; `--port N` ile değişir,
  kalıcı serviste `kernelman-hub.service` içindeki `7443`). Sertifika ilk açılışta oluşturulur (kendinden imzalı);
  kendi sertifikanız için `--tls-cert` / `--tls-key`.
* Port açık değilse yukarıdaki `firewall-cmd` satırını root olarak çalıştırın.
* Tarayıcıda linki açın → ilk girişte **yönetici hesabı** oluşturulur (en az 10 karakter parola).
  Bu parola aynı zamanda saklanan SSH parolalarını şifreleyen anahtardır.
* Kalıcı servis: paketteki `kernelman-hub.service` dosyasının başındaki 3 komut (systemd, ayrı `kernelman` kullanıcısı).
* Veri dizini: `~/.config/KernelManHub` (servis olarak `/var/lib/kernelman-hub`).

## 1b. Yalnızca konsol (tarayıcı ve port yok)

```sh
./kernelman-hub console                 # RHEL'de ssh oturumundan; port açılmaz, tarayıcı gerekmez
./kernelman-hub console --demo          # simüle edilmiş hostlarla deneme (admin / kernelman1)
```

Dashboard ile **aynı envanter, kasa, iş geçmişi ve güvenlik kuralları** kullanılır (aynı `--data` dizini; ilk açılışta
yönetici hesabı konsoldan oluşturulur). Menü: `1` durum yenile · `2` sistem detayı (hostlar, instance'lar, local exe = central,
disk) · `3` işlem (Kernel Update, Backup, Transfer files, Stop, Start, Start services, Rollback): kilit açma → arşivleri numarayla
seçme → host seçimi → **plan** → stop/update/rollback için **SID'i yazma** → canlı konsol, her adımda yüzde ve terminalin
altında ilerleme çubuğu (`#######......  42%  Stop SAP on prdapp2`) · `4` hostlar (ekle/düzenle/sil/test; root · `<sid>adm`,
sudo · dzdo · `dzdo su -`) · `5` arşivler (sunucudaki bir dosyayı veya dizini kütüphaneye al) · `6` işler ve logları · `7` ayarlar.
Ctrl+C iş sürerken **mevcut adım bittikten sonra** durdurur (SAPCAR/yedek yarıda kesilmez).
Aynı veri dizinini aynı anda tek hub süreci kullanabilir (dashboard **veya** konsol): ikincisi açıkça reddedilir.

## 2. Windows'ta çalıştırma

`kernelman-hub-windows-amd64.zip` → `kernelman-hub.exe` çift tıklayın. Tarayıcı kendiliğinden açılır
(`http://127.0.0.1:8090`, yalnızca bu PC). Ağa açmak için: `kernelman-hub.exe --listen 0.0.0.0:7443`.

Denemek için SAP'siz demo: `kernelman-hub --demo` (giriş `admin` / `kernelman1`, simüle edilmiş hostlar).

## 3. Host ekleme — root gerekmez

**Hosts → Add host:** host adı/IP, giriş kullanıcısı, parola (veya SSH anahtarı) ve **"Run operations as"**:

| Giriş | Run operations as | Ne zaman |
|---|---|---|
| `devadm` (sidadm) | the login user | sidadm parolası biliniyor |
| domain kullanıcınız | `<sid>adm` + Become with `sudo` | `sudo -u devadm` yetkiniz var |
| domain kullanıcınız | `root` + Become with `sudo` / `dzdo` | sudo veya Centrify dzdo ile root yetkiniz var |
| domain kullanıcınız | `root` + Become with **`dzdo su -`** | yalnızca `dzdo su -` izinliyse (komut `dzdo su - root -c '…'` olarak gider) |
| `root` | the login user | root parolası paylaşılıyorsa |

* **Önerilen (saproot.sh dahil her şey):** `root` + `dzdo` (olmazsa `dzdo su -`). Ajan root'tan dosya işlerini ve
  sapcontrol'ü `su - <sid>adm` ile yapar, `chown` ve `saproot.sh`'yi kendisi çalıştırır.
* sudo/dzdo parola istiyorsa **"sudo / dzdo asks for my password"** kutusunu işaretleyin: giriş parolası
  standart girişten (`-S`) verilir, komut satırına asla yazılmaz.
* sidadm olarak her SAP adımı çalışır; yalnızca root gerektirenler (chown, `saproot.sh`, SAP Host Agent restart)
  "run as root later" olarak raporlanır.
* sudo modlarında ajan `/var/tmp/kernelman-<kullanıcı>` dizinine kurulur (sidadm sizin ev dizininizi okuyamaz);
  ayarlardaki arşiv dizinine (`/usr/sap/download/kernelman`) yazılamıyorsa arşivler `/var/tmp/kernelman-<kullanıcı>/download`'a
  gider ve SAP durdurulmadan önce `sudo -u <sid>adm cksum` ile **çalıştıran kullanıcının da okuyabildiği** kanıtlanır.
  `/var/tmp` `noexec` bağlıysa hub bunu açıkça söyler.
* Host eklemek **yalnızca okur**: SSH ile girer, ajanı kopyalar, `kernelman status` çalıştırır.
  Host anahtarı ilk bağlantıda kaydedilir; değişirse bağlantı reddedilir.

## 4. Güvenlik kuralları (hub bunları zorlar)

1. Her SAP sistemi **salt-okunurdur**; değişiklik için sistem kartında **Unlock for changes** (8 saat sonra kendiliğinden kilitlenir).
   PRD'yi bakım penceresine kadar kilitli tutun.
2. Stop, Kernel Update ve Rollback **SID'nin yeniden yazılmasını** ister ve çalışmadan önce **planı** gösterir.
3. Sistemin bir app sunucusu hub'da yoksa Kernel Update **reddedilir** (geride kalan exe dizini olmasın).
4. **Ön kontrol**: değişiklikten önce her hostta sistem var mı, DIR_CT_RUN biliniyor mu, `/usr/sap` ve `/sapmnt`'de en az 2 GB boş mu.
5. **Son doğrulama**: çıkarmadan sonra her instance `exe` dizini merkezi kernel ile dosya dosya karşılaştırılır;
   fark varsa SAP **başlatılmaz**.
6. Aynı anda tek iş; **durdurma** isteği çalışan adımı bitirir (SAPCAR ve yedek yarıda kesilmez), sonrakileri atlar.
7. Arşivler hosta gönderildikten sonra `cksum` ile doğrulanır; SSH parolaları AES-256-GCM ile şifreli saklanır.

## 5. Kernel güncelleme akışı

1. SAP Software Center'dan arşivleri bu PC'ye indirin → **Archives** sayfasına sürükleyin (SAPCAR da eklenebilir).
2. Sistem kartını açın → **Unlock for changes** → **Kernel Update**.
3. Arşivleri seçin, planı inceleyin, SID'yi yazın → **Run**.
4. Canlı konsolda her adımı izleyin. Sıra: ön kontrol → arşivleri gönder → stop → (her hostta) yedek + çıkarma +
   doğrulama → bütün hostlarda kernel seviyesi kontrolü → start.
