# M365Bridge

[![CI](https://github.com/KilimcininKorOglu/M365Bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/KilimcininKorOglu/M365Bridge/actions/workflows/ci.yml)
[![Release](https://github.com/KilimcininKorOglu/M365Bridge/actions/workflows/release.yml/badge.svg)](https://github.com/KilimcininKorOglu/M365Bridge/actions/workflows/release.yml)
[![Version](https://img.shields.io/github/v/release/KilimcininKorOglu/M365Bridge)](https://github.com/KilimcininKorOglu/M365Bridge/releases)
[![Docker](https://img.shields.io/badge/docker-ghcr.io-blue)](https://github.com/KilimcininKorOglu/M365Bridge/pkgs/container/m365bridge)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![OpenAI Uyumlu](https://img.shields.io/badge/API-OpenAI%20Compatible-412991)](#api-uç-noktaları)
[![Anthropic Uyumlu](https://img.shields.io/badge/API-Anthropic%20Compatible-D97757?logo=anthropic&logoColor=white)](#api-uç-noktaları)

**[English](README.md)** | **Türkçe**

Microsoft 365 Copilot'un WebSocket arayüzünü OpenAI/Anthropic uyumlu HTTP API'sine dönüştüren bir Go uygulamasıdır.

![Tarayıcı arayüzü, bir soruyu kaynak göstererek yanıtlıyor](docs/webui-tr.png)

## Mimari

Uygulamanız -> M365Bridge -> substrate.office.com (SignalR) -> M365 Copilot Backend

## Ön Koşullar

- **Go 1.26+** kurulu ([indir](https://go.dev/dl/)). 1.21 ve sonrası daha eski bir Go da çalışır: ilk derlemede 1.26 toolchain'ini indirir, `GOTOOLCHAIN` değeri `local` değilse.
- Bu repoyu klonlamak için **git**
- **Microsoft 365 Copilot lisansı** (iş veya kurumsal hesap, Copilot erişimi olan) test edilmiş copilot chat (temel) hesabı
- [https://m365.cloud.microsoft](https://m365.cloud.microsoft) adresine giriş yapmış bir tarayıcı (kurulum sihirbazı token çıkarımı için)

## Özellikler

- Akışlı/akışsız çıktı ile metin sohbeti
- Çok modlu görsel girdi (OpenAI `image_url` ve Anthropic `image` içerik blokları; PNG, JPEG, GIF, WebP)
- Microsoft Designer üzerinden görsel üretimi (`/v1/images/generations`, `/v1/images/edits`), `url` ve `b64_json` yanıt formatları desteklenir
- ConversationId takibi ile çok turlu sohbet desteği
- Oturum izolasyonu (oturum başına ayrı M365 sohbetleri)
- Düşünme/akıl yürütme içeriği çıkarımı (OpenAI için `reasoning_content`, Anthropic için `thinking` blokları)
- Simüle edilmiş tool calling (istemci tanımlı araçlar hem OpenAI hem Anthropic uç noktalarında, streaming ve non-streaming modlarda çalışır)
- OpenAI uyumlu API uç noktaları, Responses API ve compaction route'u dahil
- Anthropic uyumlu API uç noktaları (özel SSE işleyiciler)
- `/mcp` üzerinde Model Context Protocol sunucusu (JSON-RPC 2.0)
- Gateway'in yerelde çalıştırdığı yerleşik coding tool'lar; `M365_ENABLE_CODE_TOOLS` açmadıkça kapalı
- Her sohbet uç noktasında stop sequence, cevap akarken kesilir
- API anahtarı kimlik doğrulama (`M365_API_KEYS` / `M365_API_KEY`)
- Tüm uç noktalarda max_tokens uygulaması (tiktoken BPE)
- `/v1/quota` üzerinde sohbet kotası sayaçları
- Etkileşimli kullanım için CLI arayüzü
- Binary'ye gömülü tarayıcı arayüzü (konuşma listesi, akışlı sohbet, model seçimi, markdown cevaplar, İngilizce ve Türkçe)
- Alt komut yönlendirmeli tek binary

## Kurulum

Servisi çalıştırmanın üç yolu var: kaynaktan derlemek, hazır binary indirmek veya Docker image'ini çalıştırmak. Üçü de aynı tek seferlik tarayıcı token kurulumunu gerektirir.

### Docker Olmadan

Docker zorunlu değildir. Binary'yi doğrudan çalıştırmak için, Windows, macOS veya Linux'ta:

#### Adım 1: Binary'yi derleyin

```bash
git clone https://github.com/KilimcininKorOglu/M365Bridge
cd M365Bridge
go build -o bin/m365-bridge ./cmd/cli
```

Windows'ta çıktı adını `bin/m365-bridge.exe` yapın.

#### Adım 2: data dizinini oluşturun

```bash
mkdir data
```

Bütün runtime yolları çalışma dizinine görelidir, bu yüzden **aşağıdaki her komutu repo kökünden çalıştırın**. Binary'yi başka bir dizinden başlatmak `data/` dizinini orada aratır ve eksik token hatası verir.

#### Adım 3: Kimlik doğrulama token'ınızı alın

Tarayıcı snippet'ini çalıştırmak için yukarıdaki Docker bölümünün **Adım 3**'ünü izleyin. Token'ı 24 saatten sonra da yenileyen SSO cookie'lerini istiyorsanız **Adım 4**'ü de uygulayın.

#### Adım 4: setup.json oluşturun ve sihirbazı çalıştırın

Tarayıcı çıktısını Docker bölümünün **Adım 5**'inde gösterilen biçimde `data/setup.json` dosyasına kaydedin, sonra:

```bash
./bin/m365-bridge setup-wizard
```

Windows PowerShell:

```powershell
.\bin\m365-bridge.exe setup-wizard
```

Sihirbaz `data/.env` dosyasını ve `data/tokens/` altındaki şifreli kimlik bilgilerini yazar.

#### Adım 5: Sunucuyu başlatın

```bash
./bin/m365-bridge serve --port 8000
```

Windows PowerShell:

```powershell
.\bin\m365-bridge.exe serve --port 8000
```

API `http://localhost:8000` adresinde çalışır. Docker kurulumu container'ı host portu 8230'a eşler; doğrudan çalıştırmada eşleme yoktur, verdiğiniz port kullandığınız porttur.

### Hazır Binary'ler

Platformunuz için en son binary'yi [GitHub Releases](https://github.com/KilimcininKorOglu/M365Bridge/releases) sayfasından indirin:

| Platform                    | Dosya                           |
|-----------------------------|---------------------------------|
| Linux amd64                 | `m365-bridge-linux-amd64`       |
| Linux arm64                 | `m365-bridge-linux-arm64`       |
| macOS amd64 (Intel)         | `m365-bridge-darwin-amd64`      |
| macOS arm64 (Apple Silicon) | `m365-bridge-darwin-arm64`      |
| Windows amd64               | `m365-bridge-windows-amd64.exe` |
| Windows arm64               | `m365-bridge-windows-arm64.exe` |

```bash
# Örnek: Linux amd64
wget https://github.com/KilimcininKorOglu/M365Bridge/releases/latest/download/m365-bridge-linux-amd64
chmod +x m365-bridge-linux-amd64
./m365-bridge-linux-amd64 serve --port 8000
```

### Docker

M365Bridge'i çalıştırmanın en kolay yolu Docker'dır. Hazır imaj GitHub Container Registry'de mevcuttur.

#### Adım 1: docker-compose.yml oluşturun

Proje dizininizde bir `docker-compose.yml` dosyası oluşturun:

```yaml
services:
  m365bridge:
    image: ghcr.io/kilimcininkoroglu/m365bridge:latest
    container_name: m365bridge
    ports:
      - "8230:8000"
    volumes:
      - ./data:/app/data
    restart: unless-stopped
```

#### Adım 2: Container'ı başlatın

```bash
docker compose up -d
```

API `http://localhost:8230` adresinde erişilebilir olacaktır.

#### Adım 3: Tarayıcıdan kimlik doğrulama token'ını alın

Sunucunun, Microsoft 365 Copilot oturumunuzdan bir refresh token'a ihtiyacı var. Şu şekilde çıkarın:

1. Tarayıcınızda [https://m365.cloud.microsoft](https://m365.cloud.microsoft) adresini açın ve giriş yapın
2. DevTools'u açmak için **F12**'ye basın, **Console** sekmesine geçin
3. Aşağıdaki JavaScript kodunu yapıştırıp çalıştırın:

<details>
<summary>JavaScript çıkarma kod parçacığını genişletmek için tıklayın</summary>

```javascript
(async () => {
// 1. Get oid/tenant from the signed-in account
let oid, tenant;
for (const key of Object.keys(localStorage)) {
  if (!key.includes('active-account-filters')) continue;
  try {
    const val = JSON.parse(localStorage.getItem(key));
    if (val?.homeAccountId?.includes('.')) { [oid, tenant] = val.homeAccountId.split('.'); break; }
  } catch(e) {}
}
if (!oid) {
  const mk = Object.keys(localStorage).find(k => k.startsWith('msal.') && k.includes('|'));
  if (mk) { const p = mk.split('|')[1]; if (p?.includes('.')) [oid, tenant] = p.split('.'); }
}
if (!oid || !tenant) return 'ERROR: No signed-in account found. Log in to m365.cloud.microsoft and run this again.';

// 2. Watch every token exchange for the one this gateway uses
const targetClientID = '4765445b-32c6-49b0-83e6-1d93765276ca';
const origFetch = window.fetch;
let done;
const captured = new Promise(resolve => { done = resolve; });
window.fetch = async function(...args) {
  const resp = await origFetch.apply(this, args);
  const url = typeof args[0] === 'string' ? args[0] : args[0]?.url || '';
  if (url.includes('oauth2/v2.0/token')) {
    try {
      let bodyStr = '';
      const init = args[1];
      if (typeof init?.body === 'string') bodyStr = init.body;
      else if (init?.body instanceof URLSearchParams) bodyStr = init.body.toString();
      else if (init?.body instanceof ArrayBuffer || ArrayBuffer.isView(init?.body)) bodyStr = new TextDecoder().decode(init.body);
      else if (args[0] instanceof Request) bodyStr = await args[0].clone().text();
      // The sign-in exchange puts a broker id in client_id and carries the real
      // target in brk_client_id, so both are accepted.
      const params = new URLSearchParams(bodyStr);
      const isTarget = params.get('client_id') === targetClientID
                    || params.get('brk_client_id') === targetClientID;
      if (isTarget) {
        const data = await resp.clone().json();
        if (data.refresh_token) {
          console.log('===== COPY THE COMPLETE JSON BELOW =====');
          console.log(JSON.stringify({oid, tenant, refresh_token: data.refresh_token}, null, 2));
          done(true);
        }
      }
    } catch(e) {}
  }
  return resp;
};

// 3. Make the app ask for a token
// The page keeps its MSAL instance out of reach of the console, so the refresh
// cannot be requested directly. Moving to another route makes the app request
// one; the original page is restored afterwards.
const startPath = location.pathname;
let moved = false;
for (const href of ['/search', '/library', '/teach', '/chat/all', '/chat']) {
  if (href === startPath) continue;
  const link = document.querySelector('a[href="' + href + '"]');
  if (link) { link.click(); moved = true; break; }
}

// 4. Wait for the exchange, then put everything back
const ok = await Promise.race([captured, new Promise(r => setTimeout(() => r(false), 20000))]);
if (moved) history.back();
window.fetch = origFetch;

return ok
  ? 'Done. Copy the JSON printed above.'
  : 'No token exchange seen; the app is still using a token it refreshed a moment ago. Reload the page and run this again.';
})()
```

</details>

4. Birkaç saniye bekleyin. Snippet, uygulamanın token istemesi için kendiliğinden başka bir sayfaya gidip geri döner, sonra şunu yazdırır: `===== COPY THE COMPLETE JSON BELOW =====`
5. JSON çıktısını kopyalayın. Şu formatta olacaktır:

```json
{
  "oid": "sizin-oid",
  "tenant": "sizin-tenant",
  "refresh_token": "sizin-refresh-token"
}
```

> **Not:** Snippet SSO cookie'lerini okuyamaz. Bu cookie'ler bu sayfada değil `login.microsoftonline.com` üzerinde bulunur ve `HttpOnly` işaretlidir, yani hiçbir sayfadaki hiçbir script onları okuyamaz. Adım 4 onları el ile toplar.

#### Adım 4 (Önerilir): SSO cookie'leri alın

Bu iki cookie olmadan kurulum 24 saat sonra çalışmayı bırakır. Onları el ile toplayın:

Microsoft SPA refresh token'ları **24 saat** sonra süresi dolar. SSO cookie'leri olmadan, 24 saatte bir Adım 3'ü tekrarlamanız gerekir. SSO cookie'leri otomatik yenilemeyi sağlar ve haftalarca/aylarca dayanır.

SSO cookie'lerini yakalamak için:

1. Tarayıcınızda [https://login.microsoftonline.com](https://login.microsoftonline.com) adresini açın (cookie'ler burada bulunur, m365.cloud.microsoft'ta değil)
2. DevTools'u açmak için **F12**'ye basın, **Application** > **Cookies** > `https://login.microsoftonline.com` bölümüne gidin
3. Şu iki cookie'nin değerlerini bulun ve kopyalayın:
   - `ESTSAUTH`
   - `ESTSAUTHPERSISTENT`

#### Adım 5: setup.json oluşturun

Adım 3'teki JSON ile `data/setup.json` dosyası oluşturun. Adım 4'te SSO cookie'leri manuel yakaladıysanız, `sso_cookies` dizisine ekleyin:

**SSO cookie'leri olmadan (24 saatte bir setup tekrar gerekir):**

```json
{"oid":"sizin-oid","tenant":"sizin-tenant","refresh_token":"sizin-refresh-token"}
```

**SSO cookie'leri ile (otomatik yenileme, önerilir):**

```json
{
  "oid": "sizin-oid",
  "tenant": "sizin-tenant",
  "refresh_token": "sizin-refresh-token",
  "sso_cookies": [
    {"name": "ESTSAUTH", "value": "estsauth-degerini-buraya-yapistirin"},
    {"name": "ESTSAUTHPERSISTENT", "value": "estsauthpersistent-degerini-buraya-yapistirin"}
  ]
}
```

#### Adım 6: Kurulum sihirbazını çalıştırın

Kimlik bilgilerinizi şifreleyip kaydetmek için container içinde kurulum sihirbazını çalıştırın:

```bash
docker exec -it m365bridge ./bin/m365-bridge setup-wizard
```


Sihirbaz şunları yapar:
- `data/setup.json` dosyasını okur
- Refresh token ve SSO cookie'lerini AES-256-GCM ile şifreler
- Ortam değişkenlerini `data/.env` dosyasına kaydeder
- Token'ı access token ile değiştirerek doğrular

Başarı durumunda sunucu hazırdır. API `http://localhost:8230` adresinde kullanılabilir.

> **Not:** SSO cookie'leri yakalamadıysanız, refresh token 24 saat sonra süresi dolar ve sunucu çalışmayı durdurur. Yeni token almak için Adım 3, 5 ve 6'yı tekrarlayın. SSO cookie'leri ile sunucu, token süresi dolduğunda otomatik olarak yeniler.

#### Alternatif: docker run

Docker Compose yerine `docker run` kullanmayı tercih ederseniz:

```bash
docker run -d \
  --name m365bridge \
  -p 8230:8000 \
  -v $(pwd)/data:/app/data \
  --restart unless-stopped \
  ghcr.io/kilimcininkoroglu/m365bridge:latest
```

Sonra yukarıdaki Adım 3-6'yı izleyin.

#### Notlar

- `data/` dizini token'ları, önbelleği ve yapılandırmayı saklar. İlk çalıştırmada otomatik oluşturulur.
- Port `8230` (host) ile `8000` (container) arasında eşleştirilir. Host portunu `docker-compose.yml` veya `-p` parametresinden değiştirebilirsiniz.
- Container varsayılan olarak `serve --port 8000` ile başlar.
- Hazır imaj yerine kendiniz derlemek isterseniz: `docker compose up --build -d`

## Kullanım

### CLI Bayrakları

| Bayrak          | Tip    | Varsayılan | Açıklama                                                                                                                                                                                 |
|-----------------|--------|------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `-i`            | bool   | false      | Etkileşimli mod (çok turlu sohbet)                                                                                                                                                       |
| `--model`       | string | `auto`     | Kullanılacak model: `auto`, `quick`, `reasoning`, `gpt5.2`, `gpt5.2-reasoning`, `gpt5.3`, `gpt5.4`, `gpt5.4-reasoning`, `gpt5.5`, `gpt5.5-reasoning`, `gpt5.6-reasoning`, `claude`, `claude-sonnet`, `claude-opus`, `claude-sonnet-4-20250514` |
| `--reasoning`   | bool   | false      | Akıl yürütme modunu kullan                                                                                                                                                               |
| `--no-stream`   | bool   | false      | Akışı devre dışı bırak, tam yanıtı tek seferde yazdır                                                                                                                                    |
| `--list-models` | bool   | false      | Tüm kullanılabilir modelleri listele ve çık                                                                                                                                              |
| `--version`     | bool   | false      | Sürümü göster ve çık                                                                                                                                                                     |

Konumsal argüman (hiçbir bayrak tüketmezse): tek sorgu modu için sorgu metni.

### Alt komut: serve

HTTP API sunucusunu başlatır.

| Bayrak      | Tip  | Varsayılan | Açıklama             |
|-------------|------|------------|----------------------|
| `--port`    | int  | 8000       | Dinlenecek port      |
| `--version` | bool | false      | Sürümü göster ve çık |

### Alt komut: setup-wizard

Tarayıcı tabanlı kurulum sihirbazını çalıştırır. `oid`, `tenant` ve `refresh_token` içeren JSON dosyasını okur.

| Bayrak   | Tip    | Varsayılan        | Açıklama                     |
|----------|--------|-------------------|------------------------------|
| `--file` | string | `data/setup.json` | Kurulum JSON dosyasının yolu |

Her bayrak isteğe bağlıdır. Hiçbiri verilmezse `serve` 8000 portunu dinler, `setup-wizard` ise `data/setup.json` dosyasını okur.

### Temel Ortam Değişkenleri

Yapılandırma `data/.env` dosyasından okunur; process ortam değişkeni dosyadaki değerin önüne geçer. İlk ikisini kurulum sihirbazı yazar.

| Değişken         | Varsayılan                             | Açıklama                                                                                                       |
|------------------|----------------------------------------|------------------------------------------------------------------------------------------------------------------|
| `M365_TENANT_ID` | zorunlu                                | Directory (tenant) ID. CLI de sunucu da bu değer olmadan çıkar.                                                |
| `M365_USER_OID`  | zorunlu                                | Oturum açan kullanıcının object ID değeri. CLI de sunucu da bu değer olmadan çıkar.                            |
| `M365_CLIENT_ID` | `4765445b-32c6-49b0-83e6-1d93765276ca` | Access token'ların düzenlendiği OAuth client. Yalnızca varsayılanı engelleyen bir tenant için değiştirin.       |
| `M365_API_KEYS`  | tanımsız                               | İstemcinin sunması gereken anahtarlar, virgülle ayrılır. Tanımsızken tüm `/v1/*` rotaları ve `/mcp` açıktır.    |
| `M365_API_KEY`   | tanımsız                               | Tek anahtar; yalnızca `M365_API_KEYS` tanımsızken okunur.                                                       |
| `TZ`             | sistem saat dilimi                     | Her turla gönderilen saat dilimi. Yoksa `/etc/localtime` üzerinden, o da yoksa UTC olarak belirlenir.           |

Kalan değişkenleri aşağıdaki bölümler, değiştirdikleri davranışın yanında belgeliyor. `m365-bridge --help` komutu hepsini güncel varsayılanlarıyla tek listede yazdırır.

### Örnekler

```bash
# Tek sorgu
./bin/m365-bridge "soru metniniz"

# Etkileşimli mod
./bin/m365-bridge -i

# Akıl yürütme ile model belirtme
./bin/m365-bridge --model gpt5.5-reasoning "soru metniniz"

# Akışsız
./bin/m365-bridge --no-stream "soru metniniz"

# Modelleri listele
./bin/m365-bridge --list-models

# API sunucusunu başlat
./bin/m365-bridge serve --port 8000

# Özel dosya ile kurulum sihirbazını çalıştır
./bin/m365-bridge setup-wizard --file /path/to/setup.json
```

### API Sunucusu

```bash
# 8000 portunda API sunucusunu başlat
./bin/m365-bridge serve --port 8000

# curl ile test (kimlik doğrulamasız)
curl http://127.0.0.1:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"Merhaba"}]}'

# curl ile test (API anahtarı ile)
curl http://127.0.0.1:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{"model":"gpt5.5","messages":[{"role":"user","content":"Merhaba"}]}'

# Oturum izolasyonu ile akış
curl http://127.0.0.1:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -H "X-Session-Id: my-session-1" \
  -d '{"model":"gpt5.5","stream":true,"messages":[{"role":"user","content":"Merhaba"}]}'
```

### İlk Çalıştırma

Sunucuyu ilk kez başlattığınızda:

1. Sunucu geçerli çalışma dizininden `data/.env` dosyasını okur
2. `data/tokens/rt_90day.txt` dosyasından şifrelenmiş refresh token'ı yükler
3. Token yenileme gerçekleştirir (refresh token'ı access token'a değiştirir). Bu 1-2 saniye sürer
4. Başarı durumunda `Starting API server on port 8000 (no API key required)` görürsünüz; anahtar tanımlıysa `(API key required, N key(s) configured)` yazar
5. İlk istek, `substrate.office.com`'a WebSocket bağlantısı açtığı için biraz daha uzun sürebilir

Refresh token eksik veya süresi dolmuşsa, sunucu `data/tokens/sso_cookies.json` dosyası mevcutsa SSO cookie ile yeniden kimlik doğrulamayı dener. SSO cookie'leri de yoksa veya süresi dolmuşsa, sunucu token yenileme hatası ile başlayamaz. Taze token ve cookie çıkarmak için `./bin/m365-bridge setup-wizard` komutunu tekrar çalıştırın.

### Oturum İzolasyonu

Her oturum benzersiz bir M365 sohbetine eşlenir. Oturum ID'si öncelik sırasına göre çözümlenir:

1. Model adında iki nokta üst üsteden sonraki `sessionID` (`model:sessionID`)
2. İstek gövdesinde `previous_response_id` alanı (yalnızca `/v1/responses`)
3. İstek gövdesinde `session_id` alanı
4. İstek gövdesinde `user` alanı
5. `X-Session-Id` başlığı
6. `X-Claude-Code-Session-Id` başlığı (Claude Code) veya `session-id` başlığı (Codex)
7. `hash(api_key + ilk_kullanıcı_mesajı)` (kimlik doğrulama açıkken) veya `hash(ilk_kullanıcı_mesajı)` (kimlik doğrulama kapalıyken)

Her endpoint session'ı bu tek sıraya göre çözümler.

Claude Code ve Codex, bir oturumun her isteğine kendi oturumunu damgalar. Başlık adı sabittir, hiçbiri değiştirilmek üzere ayarlanamaz. 4. adım bu iki adı okur, böylece her iki istemci de hiçbir yapılandırma olmadan oturum başına tek sohbet tutar. Üstündeki alanların altında yer alır, çünkü istemci o başlığı kendiliğinden yazar; üstündeki her değeri ise çağıran bilerek koyar.

Codex ayrıca `session-id` ile aynı değeri taşıyan `thread-id` başlığını gönderir. Onu okumak yalnızca `session-id` zaten taşıyan bir istek için cevap verirdi. `x-codex-turn-metadata` başlığı hiç okunmaz: içindeki `installation_id` bir makinedeki her oturum boyunca aynı kalır, bir sohbeti ona bağlamak ilgisiz oturumları tek sohbette birleştirirdi.

Hash yedeği diğer istemcileri kapsar, ilk kullanıcı mesajları farklı olduğu sürece.

`GET /v1/sessions` eşlemeleri en yeniden eskiye listeler. Eşlemenin kendi oturum ID'sini taşımasından önce yazılmış kayıtlar listelenemez, çünkü cache dosya adı anahtarın hash'idir; bunlar `legacy_entries` sayısı olarak bildirilir ve bir sonraki tur onları yeniden yazdığında listede görünür.

`DELETE /v1/sessions/{id}` önce upstream M365 sohbetini siler, sonra eşlemeyi temizler; böylece o oturum ID'si ile atılan bir sonraki tur yeni bir sohbet başlatır. Upstream silme başarısız olursa eşleme korunur, istek tekrarlanabilir. Sohbeti silmek `data/tokens/m365_cookies.json` içindeki M365 web cookie'lerini gerektirir; yalnızca eşlemeyi temizleyip sohbeti yerinde bırakmak için `?local_only=true` ekleyin. Bu cookie'leri olmayan bir kurulumun ihtiyaç duyduğu yol budur.

### Sistem Talimatları

M365 backend'i konuşma geçmişini kendisi tutar ve yalnızca en son turu alır; bu yüzden daha önceki bir mesajda gönderilen talimat ona hiç ulaşmaz. Bu nedenle istekteki her `system` mesajı toplanır ve o turun önüne eklenir. Aynı mesaj düzleştirilmiş geçmişin dışında tutulur, çünkü orada geçmiş bir konuşma satırı gibi okunurdu.

`developer` rolü aynı şekilde ele alınır. OpenAI, reasoning modelleri için rolü yeniden adlandırdı ve iki ad da geçerli kaldı; bu yüzden hangisini gönderirse göndersin istemci modele aynı şekilde ulaşır.

Anthropic'in üst düzey `system` alanı string veya metin bloğu dizisi olarak kabul edilir ve aynı ön ek talimatına dönüşür.

### Python İstemcisi (OpenAI SDK)

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8000/v1",
    api_key="your-api-key",  # M365_API_KEYS ayarlıysa zorunlu
)
resp = client.chat.completions.create(
    model="gpt5.5",
    messages=[{"role": "user", "content": "Merhaba"}]
)
print(resp.choices[0].message.content)
```

### Python İstemcisi (Anthropic SDK)

```python
from anthropic import Anthropic

client = Anthropic(
    base_url="http://127.0.0.1:8000",
    api_key="your-api-key",  # M365_API_KEYS ayarlıysa zorunlu
)
resp = client.messages.create(
    model="gpt5.5",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Merhaba"}]
)
print(resp.content[0].text)
```

Anthropic SDK `/v1/messages` yolunu kendisi ekler, bu yüzden base URL host'ta biter.

### Görsel Girdi Örneği

```python
from openai import OpenAI
import base64

client = OpenAI(
    base_url="http://127.0.0.1:8000/v1",
    api_key="your-api-key",
)

with open("image.png", "rb") as f:
    img_b64 = base64.b64encode(f.read()).decode()

resp = client.chat.completions.create(
    model="gpt5.5",
    messages=[{
        "role": "user",
        "content": [
            {"type": "text", "text": "Bu görselde ne var?"},
            {"type": "image_url", "image_url": {"url": f"data:image/png;base64,{img_b64}"}},
        ],
    }],
)
print(resp.choices[0].message.content)
```

## Tarayıcı Arayüzü

Sunucunun kök adresini tarayıcıda açın (varsayılan Docker kurulumunda `http://localhost:8230/`). Arayüz binary'ye gömülüdür, bu yüzden ayrı bir asset dizini ve çalıştırılacak ikinci bir süreç yoktur.

Sol tarafta konuşmaları listeler, cevapları geldikçe akıtır, `GET /v1/models` listesinden model seçtirir, konuşma oluşturur, yeniden adlandırır ve siler. Cevap markdown olarak çizilir, bu yüzden karşılaştırma tablosu tablodur ve kaynak, cümlenin ortasındaki bir URL değil bağlantıdır. Sizin yazdığınız metin ise aynen yazdığınız gibi gösterilir. Yeniden adlandırma ve silme, tarayıcının kendi diyalogları yerine sayfa içinde sorar.

Sayfanın ihtiyaç duyduğu her şey binary'ye derlenmiştir. Başka bir yerden font, script veya stylesheet yüklemez; bu yüzden M365 backend'i dışında internete çıkışı olmayan bir makinede de çalışır.

Sayfanın kendisi API anahtarı olmadan sunulur, çünkü anahtarı soran ekran anahtar isteyemez. Yaptığı tüm veri çağrıları diğer istemcilerle aynı `withAuth` middleware'inden geçer. Anahtar cookie'de saklanır ve `Authorization` başlığıyla gönderilir; tarayıcının kendiliğinden eklediği bir cookie olarak asla gitmez, bu yüzden siteler arası bir istek onu taşıyamaz.

### Sidebar neyi gösterir

İki kaynak birleştirilir. `GET /v1/conversations` isimleri verir ve M365 web cookie'leri gerektirir; `GET /v1/sessions` konuşmayı devam ettirilebilir kılan oturum ID'lerini verir. İkisinde de bulunan bir konuşma tek satırdır.

Cookie yoksa ilk çağrı başarısız olur, sidebar yalnızca yerel eşlemelere düşer ve bunu yazar. Sadece M365'in bildiği bir konuşma işaretlenir ve açtığınız anda ona bir oturum ID'si bağlanır; başka bir istemcide başlamış bir konuşmayı burada devam ettirilebilir kılan budur.

### Dil

Arayüz İngilizce ve Türkçe olarak gelir; seçici, sidebar'da ismin yanındadır. Varsayılan İngilizcedir.

Her dil, `web/src/locales` altında dil koduyla adlandırılmış tek bir JSON dosyasıdır ve kodda hiçbir yer bir dosyayı ismen saymaz: build dizinin tamamını derler. Dolayısıyla dil eklemek, `en.json` dosyasını kopyalayıp değerlerini çevirmek, örneğin `de.json` olarak kaydetmek ve `make ui` çalıştırmaktan ibarettir. `$label` girdisi dili kendi dilinde adlandırır ve seçicide görünen odur. Katalogun yalnızca bir kısmını çeviren bir dosya, geri kalanı için İngilizceye düşer; böylece eksik bir çeviri bozuk değil kullanılabilir olur.

Seçilen dil `m365bridge_lang` cookie'sinde saklanır. Cookie'si olmayan bir tarayıcı ile bu build'in taşımadığı bir dili adlandıran cookie aynı durumdur: İngilizce, ve saklanan değerle gösterilen dil çelişmesin diye cookie geri yazılır.

### Transcript'ler

Backend geçmişi conversation ID ile takip eder ve asla geri göndermez, bu yüzden gateway taşıdığı turların kendi kaydını tutar: oturum başına bir dosya, `data/transcripts` altında. Mesaj içeriğinin diske ulaştığı tek yer burasıdır. Oturum başına kayıt, mesaj başına bayt ve depodaki dosya sayısı sınırlıdır.

Bu gateway dışında başlamış bir konuşmanın kaydı yoktur; açtığınızda geçmişi boş görünür. Arayüz bunu söyler ve geçmişi getirmeyi önerir; bu `GET /v1/conversations/{id}/messages` çağrısıdır (aşağıya bakın). Bir oturumu silmek transcript'ini de siler; hiçbir şey üretmeyen bir tur da siler, çünkü ikisi de o ID altında yeni bir konuşma başlatır.

### Yapılandırma

| Değişken              | Varsayılan | Açıklama                                                                                        |
|-----------------------|------------|--------------------------------------------------------------------------------------------------|
| `M365_ENABLE_WEB_UI`  | `1`        | Arayüzü `/` altında sunar ve transcript kaydeder. `0`, `false`, `off` veya `no` ikisini de kapatır. |

Kapatmak arayüzü kaldırır (`/` 404 döner) ve kaydı durdurur; yalnızca proxy olarak çalışan bir kurulumun istediği budur. Bu durumda `GET /v1/sessions/{id}/messages` `404 transcripts_disabled` döndürür.

### Arayüzü derlemek

Kaynaklar `web/` altında, build çıktısı `pkg/webui/dist` altında commit'lidir, çünkü `go:embed` onu derleme sırasında okur. `web/` altında bir şey değiştirdikten sonra yeniden derleyin:

```bash
make ui      # node container'ında derler ve çıktıyı pkg/webui/dist'e kopyalar
make up      # imajı yeniden kurar ve container'ı yeniden başlatır
```

Arayüz React, cevaplar için `remark-gfm` ile birlikte `react-markdown` ve diyaloglar için SweetAlert2 kullanır. Hepsi commit'li çıktının içine derlenir, bu yüzden sunulan sayfa çalışma zamanında hiçbir şey indirmez.

## API Uç Noktaları

| Uç Nokta                         | Açıklama                                                |
|----------------------------------|---------------------------------------------------------|
| `POST /v1/chat/completions`      | OpenAI Chat Completions (akışlı + akışsız)              |
| `POST /v1/completions`           | OpenAI metin tamamlama (akışlı + akışsız)               |
| `POST /v1/responses`             | OpenAI Responses API (akışlı + akışsız)                 |
| `POST /v1/responses/compact`     | OpenAI Responses Compact API (Codex uzaktan sıkıştırma) |
| `POST /v1/messages`              | Anthropic Messages formatı (özel SSE işleyiciler)       |
| `POST /v1/messages/count_tokens` | Anthropic girdi token'larını sayar                      |
| `POST /v1/complete`              | Anthropic Complete (FIM)                                |
| `POST /v1/images/generations`    | OpenAI Images API: metinden üret (JSON body)            |
| `POST /v1/images/edits`          | OpenAI Images API: görseli düzenle (multipart)          |
| `GET /v1/conversations`          | M365 konuşmalarını listeler (M365 web cookies gerekir)  |
| `POST /v1/conversations`         | İlk mesajla yeni bir konuşma oluşturur                  |
| `PATCH /v1/conversations/{id}`   | Konuşmayı `{ "name": "..." }` ile yeniden adlandırır    |
| `DELETE /v1/conversations/{id}`  | Konuşmayı siler ve session eşlemesini temizler          |
| `GET /v1/conversations/{id}/messages` | Upstream'de duran bir konuşmanın turlarını okur    |
| `GET /v1/models`                 | Model listesi                                           |
| `GET /v1/quota`                  | Son gözlenen M365 konuşma mesaj kotası                  |
| `GET /v1/sessions`               | Oturum-sohbet eşlemelerini listeler                     |
| `GET /v1/sessions/{id}`          | Bir oturumun sohbet ID'sini okur                        |
| Silme, hangi route'tan başlarsa başlasın konuşmayı iki taraftan da kaldırır. `DELETE /v1/conversations/{id}` o konuşmaya bağlı her session'ı ve transcript'ini temizler; `DELETE /v1/sessions/{id}` önce upstream konuşmayı siler ve eşlemeyi yalnızca bu başarısız olursa korur. Böylece çağıran, artık var olmayan bir konuşmayı gösteren bir session'la hiç karşılaşmaz.

`PUT /v1/sessions/{id}`          | Oturumu var olan bir sohbete bağlar                     |
| `GET /v1/sessions/{id}/messages` | Oturumun kayıtlı turlarını okur                         |
| `DELETE /v1/sessions/{id}`       | Sohbeti siler ve eşlemeyi temizler                      |
| `POST /mcp`                      | Model Context Protocol sunucusu (JSON-RPC 2.0)          |
| `GET /v1/health`                 | Codex için erişilebilirlik probe'u (kimlik doğrulama gerekmez) |
| `GET /health`                    | Sağlık kontrolü (kimlik doğrulama gerektirmez)          |
| `GET /`                          | Tarayıcı arayüzü (sayfa için kimlik doğrulama gerekmez) |

`PUT /v1/sessions/{id}` gövdesinde `{"conversation_id": "..."}` alır ve oturumu var olan bir sohbete yöneltir. Sohbet yolu yalnızca oturumdan sohbete çözümleme yapar, bu yüzden bu olmadan M365 web veya mobil istemcisinde başlamış bir sohbet gateway üzerinden devam ettirilemez. Var olan bir oturumu yeniden bağlamak serbesttir.

`GET /v1/sessions/{id}/messages` gateway'in o oturum için kaydettiklerini döndürür. `M365_ENABLE_WEB_UI` kapalıyken `404 transcripts_disabled` döner, başka bir yerde başlamış bir sohbet için boş liste döner.

`GET /v1/conversations/{id}/messages` bu gateway'in hiç taşımadığı bir konuşmanın turlarını okur. Backend geçmişi conversation ID altında tutar ve onu döndüren bir action sunmaz; bu yüzden okuma, M365 web istemcisinin ürettiği konuşma sayfasından yapılır ve M365 web cookie'leri gerektirir. Bir sayfa indirmesine ve bu projenin kontrol etmediği bir serialization'ı gezmeye mal olur, bu yüzden hiçbir yer bunu kendiliğinden çağırmaz. `?session_id=...` eklerseniz sonuç o oturumun altına yazılır ve oturum konuşmaya bağlanır; arayüzün "geçmişi yükle" düğmesi bunu yapar. Parametre olmadan yanıt döner ve hiçbir şey yazılmaz. Okunabilir tur taşımayan bir sayfa boş konuşma yerine `502` döndürür, çünkü çağıran taraf boş konuşmayı başarısız okumadan ayırt edemez.

## Hata Yanıtları

Tüm uç noktalar hataları OpenAI hata biçiminde döndürür. `type` istemcinin dallanma yaptığı kategoridir, `code` ise makine tarafından okunabilen özel nedendir:

```json
{"error": {"message": "M365 rate limit reached for this chat request; retry after the interval in the Retry-After header", "type": "rate_limit_error", "code": "rate_limit_exceeded"}}
```

`type` değeri `invalid_request_error`, `authentication_error`, `rate_limit_error` veya `server_error` olur. Proxy'nin kendi reddettiği istekler için `code`, durum kodunun slug hâlidir; örneğin `bad_request` veya `method_not_allowed`. Tek istisna 32 MiB'ı aşan gövdedir: kendi adıyla `413 request_too_large` döner, böylece istemci küçültmesi gereken isteği bozuk bir istekten ayırabilir.

Başarısız bir backend isteği genel bir `500` yerine sınıflandırılır:

| Durum  | `code`                     | Neden                                                          |
|--------|----------------------------|----------------------------------------------------------------|
| `401`  | `upstream_auth_failed`     | Saklanan kimlik bilgileri eksik veya yenilenemedi              |
| `403`  | `insufficient_permissions` | M365 isteği yapılandırılan hesap için reddetti                 |
| `429`  | `rate_limit_exceeded`      | M365 isteği kısıtladı; `Retry-After` başlığı gönderilir        |
| `429`  | `upstream_throttled`       | Conversation mesaj kotası tükendi                              |
| `409`  | `tool_round_limit`         | Bir tur `M365_MAX_TOOL_ROUNDS` sınırından fazla tool round sürdü |
| `404`  | `model_not_found`          | İstenen model `GET /v1/models` listesinde yok                  |
| `502`  | `upstream_error`           | M365 isteği reddetti veya erişilemedi                          |
| `502`  | `upstream_unavailable`     | WebSocket handshake başarısız oldu veya bağlantı düştü         |
| `502`  | `upstream_turn_failed`     | M365 turu cevap üretmeden sonlandırdı                          |
| `502`  | `upstream_content_blocked` | M365 isteği yanıtlamak yerine reddetti                         |
| `503`  | `upstream_unavailable`     | M365 kendisini kullanılamaz olarak bildirdi                    |
| `504`  | `upstream_timeout`         | M365 zamanında yanıt vermedi                                   |

Upstream kaynaklı olduğuna dair kanıt taşımayan bir hata yine `internal_error` koduyla `500` döndürür; böylece proxy'nin kendi hatası backend arızası gibi sunulmaz. Hata mesajları sabit metindir: istek URL'leri ve kimlik bilgisi dosya yolları dahil transport hatası yalnızca sunucu log'unda kalır.

Stream açıldıktan sonra HTTP durumu zaten gönderilmiştir, bu yüzden aynı sınıflandırma gövdede taşınır. OpenAI biçimli route'lar data satırına bir `error` nesnesi koyar ve ardından `[DONE]` gönderir; `/v1/messages` ve `/v1/complete` bir `error` event'i gönderir; `/v1/responses` `response.failed` gönderir. Hiçbir route hatayı assistant içeriği olarak yazmaz, aksi hâlde istemci onu cevap olarak saklardı.

## Modeller

Tüm model seçimi, M365 backend'ine gönderilen `tone` alanı ile yapılır. Tüm modeller için `Override` alanı boştur. GPT-5.x modelleri GPT-5 backend'ine yönlendirilir. Claude tone değerleri Claude yanıtları döndürür, ancak M365 gerçek model kimliğini SignalR metadata içinde açıklamaz.

| Anahtar                    | Tone              | OpenAI ID         | Düşünme? | Backend |
|----------------------------|-------------------|-------------------|----------|---------|
| `auto`                     | Magic             | gpt-4-auto        | Hayır    | GPT-5   |
| `quick`                    | Chat              | gpt-4-quick       | Hayır    | GPT-5   |
| `reasoning`                | Gpt_5_2_Reasoning | gpt-4-reasoning   | Evet     | GPT-5   |
| `gpt5.2-reasoning`         | Gpt_5_2_Reasoning | gpt-5.2-reasoning | Evet     | GPT-5   |
| `gpt5.4-reasoning`         | Gpt_5_4_Reasoning | gpt-5.4-reasoning | Evet     | GPT-5   |
| `gpt5.2`                   | Gpt_5_2_Chat      | gpt-5.2           | Hayır    | GPT-5   |
| `gpt5.3`                   | Gpt_5_3_Chat      | gpt-5.3           | Hayır    | GPT-5   |
| `gpt5.4`                   | Gpt_5_4_Chat      | gpt-5.4           | Hayır    | GPT-5   |
| `gpt5.5`                   | Gpt_5_5_Chat      | gpt-5.5           | Hayır    | GPT-5   |
| `gpt5.5-reasoning`         | Gpt_5_5_Reasoning | gpt-5.5-reasoning | Evet     | GPT-5   |
| `gpt5.6-reasoning`         | Gpt_5_6_Reasoning | gpt-5.6-reasoning | Evet     | GPT-5   |
| `claude`                   | Claude_Sonnet     | claude-sonnet-4.6 | Hayır    | Claude  |
| `claude-sonnet`            | Claude_Sonnet     | claude-sonnet-4.6 | Hayır    | Claude  |
| `claude-opus`              | Claude_Opus       | claude-opus-4.6   | Evet     | Claude  |
| `claude-sonnet-4-20250514` | Claude_Sonnet     | claude-sonnet-4.6 | Hayır    | Claude  |

### Hangi modeli kullanmalıyım?

| Kullanım senaryosu                           | Model              |
|----------------------------------------------|--------------------|
| Genel amaçlı, backend karar versin           | `auto`             |
| Hızlı yanıtlar, basit sorular                | `quick`            |
| Karmaşık akıl yürütme, çok adımlı problemler | `reasoning`        |
| GPT-5.2 derin düşünme                        | `gpt5.2-reasoning` |
| GPT-5.4 derin düşünme                        | `gpt5.4-reasoning` |
| GPT-5.2 sohbet                               | `gpt5.2`           |
| GPT-5.3 sohbet                               | `gpt5.3`           |
| GPT-5.4 sohbet                               | `gpt5.4`           |
| GPT-5.5 sohbet                               | `gpt5.5`           |
| GPT-5.5 derin düşünme                        | `gpt5.5-reasoning` |
| GPT-5.6 derin düşünme (en yeni)              | `gpt5.6-reasoning` |
| Claude Sonnet 4.6 (Anthropic)                | `claude-sonnet`    |
| Claude Opus 4.6 (Anthropic, en yetenekli)    | `claude-opus`      |

Bir reasoning modeli, modelin düşünme sürecini içeren `reasoning_content` çıktısı üretir. OpenAI endpoint'leri bunu `reasoning_content` olarak; Anthropic endpoint'leri `text` bloğundan önce bir `thinking` içerik bloğu olarak gösterir. `claude-opus` da düşünme içeriği üretir, `claude-sonnet` üretmez. `gpt5.6-reasoning` bu yeteneği ilan eder ancak düşünme içeriği ürettiği gözlenmemiştir. İlan edilen yetenek, tone adından değil her tone'un ölçülen davranışından gelir.

### Model Adında Session ID

Model adında `:` ayırıcısı ile session ID gömebilirsiniz. Claude Code ve Codex zaten "Oturum İzolasyonu" bölümünün 4. adımıyla karşılanır; bu yola session'ı kendiniz adlandırmak istediğinizde veya hiç oturum başlığı göndermeyen bir istemci için başvurun:

```
model: "gpt5.5-reasoning:my-session-001"
```

Bu, `X-Session-Id: my-session-001` header'ı veya istek gövdesinde `session_id: "my-session-001"` ayarlamakla eşdeğerdir. Model anahtarı `:`'den önce, session ID'si sonra çıkarılır.

### Harici Model Adları

Bu gateway'in sunmadığı bir model adı `404 model_not_found` ile yanıtlanır, başka bir kayıtla asla karşılanmaz. Böylece çağıran hiç istemediği bir tone ile yanıtlanmaz.

Registry, agent istemcilerinin gönderdiği vendor adlarını taşır. Bu yüzden `claude-sonnet-4-20250514` çözümlenir; `gpt-4o` ve `o1` çözümlenmez ve `404` döner. Sunulan her id'yi `GET /v1/models` listeler.

Hiç model göndermeyen bir istek `auto` yerine, tool calling için güvenilir reasoning tone'u olan `gpt5.5-reasoning`'e düşer. Bu; boş `model` alanını, hiç gönderilmemiş alanı ve yalnızca `:session-id` ekini kapsar.

### İlan Edilen Context Window

`GET /v1/models` içindeki her kayıt, istemci harness'lerinin prompt'u veya çıktıyı önden kırpmaması için `context_window` ve `max_output_tokens` ipuçlarını ilan eder. Bunlar yalnızca istemciye yönelik ipuçlarıdır; M365 kendi sunucu tarafı limitlerini yine de uygular. İkisi de varsayılan `1000000`'dır ve override edilebilir:

| Değişken                 | Varsayılan | Açıklama                                                     |
|--------------------------|------------|--------------------------------------------------------------|
| `M365_CONTEXT_WINDOW`    | `1000000`  | `/v1/models` içinde ilan edilen context window token sayısı. |
| `M365_MAX_OUTPUT_TOKENS` | `1000000`  | `/v1/models` içinde ilan edilen maksimum çıktı token sayısı. |

### Model Listesi Alanları

`GET /v1/models` her modeli ilan edilen id'sine göre bir kez ve sıralı listeler; böylece `claude` ile `claude-sonnet` gibi takma adlar iki kez görünmez. Her kayıt şunları taşır:

| Alan                | Açıklama                                                                                                       |
|---------------------|------------------------------------------------------------------------------------------------------------------|
| `owned_by`          | Claude tone'ları için `anthropic-via-microsoft-365`, diğerleri için `microsoft-365`.                                |
| `context_window`    | `M365_CONTEXT_WINDOW` değerinden gelen ilan edilen pencere.                                                        |
| `max_output_tokens` | `M365_MAX_OUTPUT_TOKENS` değerinden gelen ilan edilen çıktı bütçesi.                                               |
| `max_input_tokens`  | Pencere eksi çıktı bütçesi; çıktı bütçesi pencereden küçük değilse pencerenin tamamı.                              |
| `supports_tools`    | Her zaman `true`; her model, çağıranın tanımladığı tool'lara simüle tool calling katmanı üzerinden erişir.         |

Yanıt ayrıca `reasoning_effort_presets` alanını taşır: her biri Responses API'nin kabul ettiği bir effort değerini adlandıran `{effort, description}` çiftleri.

Her kayıt ayrıca Codex CLI'nin okuduğu, düz OpenAI istemcilerinin yok saydığı model katalog alanlarını taşır: `base_instructions`, `model_messages`, `default_reasoning_level`, `apply_patch_tool_type`, `shell_type`, `tool_mode`, `truncation_policy`, `supports_parallel_tool_calls` ve verbosity ile reasoning-summary varsayılanları. Her yetenek hem üst seviyede hem `capabilities` altında tekrarlanır, çünkü OpenAI uyumlu istemciler bu bilgiyi farklı yerlerde arar.

#### Tek yanıtta iki wire format

Route, OpenAI ve Anthropic istemcilerine aynı anda cevap verir, çünkü iki protokol de proxy'ye aynı yoldan ulaşır. Her kayıt hem geçerli bir OpenAI model nesnesi hem de geçerli bir Anthropic `ModelInfo` nesnesidir; iki alan kümesi çakışmaz, bu yüzden her istemci yalnızca tanıdığını okur.

| Alan                | Protokol  | Açıklama                                                                 |
|---------------------|-----------|---------------------------------------------------------------------------|
| `object`            | OpenAI    | Her zaman `model`.                                                        |
| `created`           | OpenAI    | Unix saniye.                                                              |
| `owned_by`          | OpenAI    | Tone'un arkasındaki sağlayıcı.                                            |
| `shutdown_date`     | OpenAI    | Her zaman `null`; emekliye ayrılması planlanan model yok.                 |
| `type`              | Anthropic | Her zaman `model`.                                                        |
| `display_name`      | Anthropic | İnsan tarafından okunabilir ad, örneğin `Claude Sonnet 4.6`.              |
| `created_at`        | Anthropic | `created` ile aynı an, RFC 3339 biçiminde.                                |
| `max_tokens`        | Anthropic | Çıktı tavanı, `max_output_tokens` ile aynı değer.                         |

Listenin kendisi OpenAI için `object` ve `data`, Anthropic için `has_more`, `first_id` ve `last_id` taşır. Kayıt defterinin tamamı tek sayfaya sığdığı için `has_more` her zaman `false`'tur ve imleçler ilan edilen ilk ve son id'dir.

`capabilities` alanı, düz OpenAI tarzı girdilerin yanında Anthropic'in yetenek ağacını da tutar: `batch`, `citations`, `code_execution`, `context_management`, `effort`, `image_input`, `pdf_input`, `structured_outputs` ve `thinking`. Her node bir `supported` boolean'ı taşır ve üçü onun altında bir seviye daha dallanır: `effort` kabul edilen her değeri adlandırır (`low`, `medium`, `high`, `xhigh`, `max`), `thinking` kendi `types` alanını adlandırır (`enabled`, `adaptive`), `context_management` ise tarihli her stratejiyi adlandırır. Değerler proxy'nin gerçekte ne yaptığını bildirir, bu yüzden çoğu `false`'tur. `effort` yalnızca `-reasoning` varyantına yönlendirilebilen bir model için `true`'dur, `thinking` ise yalnızca chain-of-thought içeriği ürettiği ölçülen tone'lar için.

Claude Code gateway modellerini bu route üzerinden keşfeder; yalnızca Anthropic biçimini ayrıştırır ve yalnızca `claude` veya `anthropic` ile başlayan id'leri ekler. Claude tone'ları bu yüzden bu biçimde id taşır.

### Konuşma Kotası

M365, konuşma başına bir mesaj üst sınırı uygular ve sayaçları update frame'lerinde bildirir. Her tur bunları loglar, örneğin `ConvStream throttling: used=8 max=600 headroom=592`.

`GET /v1/quota` son gözlenen sayaçları döndürür. Backend bu sayaçları yalnızca bir tur devam ederken gönderir; dolayısıyla değerler canlı bir sorgu değil, en son chat isteğini yansıtır ve o isteği üreten konuşmaya aittir:

```json
{"object":"quota","available":true,"exhausted":false,"used":8,"max":600,"headroom":592}
```

Proxy'nin tanımadığı sayaçlar atılmaz, `extra` altında döndürülür. Bir istek boş upstream yanıtı üretirse ve son sayaçlar üst sınıra ulaşıldığını gösteriyorsa, proxy genel boş yanıt hatası yerine `upstream_throttled` koduyla `429` döndürür; devam etmek için yeni bir session başlatın.

### Token Kullanımı

Prompt ve completion token sayıları yerel olarak üretilen tahminlerdir; M365 backend'i kullanım bildirmez. Encoder, backend'in sunduğu GPT-5 sınıfı modellerin encoding'i olan `o200k_base`'dir; yedeği `cl100k_base`, iki sözlük de indirilemezse karakter tabanlı tahmindir. Her `usage` nesnesi sayıları hangisinin ürettiğini bildirir:

```json
{"prompt_tokens": 42, "completion_tokens": 17, "reasoning_tokens": 6, "total_tokens": 59, "usage_source": "tiktoken_o200k_base_estimate"}
```

`usage_source` ve `reasoning_tokens` standart dışı alanlardır; standart alanlar anlamını ve yerini korur. `reasoning_tokens` düşünme içeriğini sayar ve hiç üretmeyen bir tone için `0` okur. Akışlı veya akışsız, her uç nokta usage bildirir; kendi formatında usage nesnesi tanımlı olmayan `/v1/complete` dahil.

Anthropic uç noktaları aynı sayıları kendi alan adlarıyla bildirir ve aynı iki ek alanı taşır:

```json
{"input_tokens": 42, "output_tokens": 17, "reasoning_tokens": 6, "usage_source": "tiktoken_o200k_base_estimate"}
```

Akışlı bir `/v1/messages` turu bu nesneyi Anthropic wire format'ının böldüğü gibi böler: `message_start` girdi tarafını, `message_delta` çıktı tarafını taşır ve ikisi de kaynağını bildirir. Akışlı bir `/v1/complete` turu usage'ı son `completion` olayında bildirir, çünkü önceki olaylar delta taşır.

`/v1/chat/completions` ve `/v1/completions` OpenAI `stream_options` nesnesini kabul eder. `{"include_usage": false}` akışlı bir turdan usage nesnesini kaldırır. `stream_options` hiç verilmezse usage nesnesi gönderilir; bu, OpenAI'nin `false` olan kendi varsayılanından farklıdır: bu proxy her akışlı turda usage bildirmiştir ve buradaki istemciler onu okur. Prompt token'ları mesaj rolleri ve içerikleri, serileştirilmiş tool tanımları ve `tool_choice` değeri üzerinden, artı mesaj başına ve tool başına sabit bir çerçeve payı ile sayılır. `tool_choice` payı yalnızca istek tool tanımladığında uygulanır. Aynı tur bu nedenle her uç noktada aynı maliyeti verir.

### Stop Sequence'ler

Bir stop sequence, yanıtı çağıranın belirttiği yerde bitirir. Her sohbet uç noktası bunu kendi protokolünün adıyla kabul eder:

| Uç nokta                | Alan            | Şekil                       |
|-------------------------|-----------------|-----------------------------|
| `/v1/chat/completions`  | `stop`          | Bir string veya string dizisi |
| `/v1/completions`       | `stop`          | Bir string veya string dizisi |
| `/v1/messages`          | `stop_sequences`| String dizisi               |
| `/v1/complete`          | `stop_sequences`| String dizisi               |

Yanıt, içinde geçen en erken sequence'ten hemen önce kesilir ve sequence'in kendisi kaldırılır; böylece bir turu çerçeveleyen çağıran, çerçeveyi geri okumaz. Birden çok sequence verildiğinde yanıt, listede ilk yazılana değil, metinde ilk gelene göre biter. Boş sequence, sıfır konumunda eşleşmek yerine yok sayılır.

OpenAI uç noktaları, kendiliğinden biten bir yanıtla aynı olan sıradan `finish_reason: "stop"` değerini bildirir. Anthropic uç noktaları `stop_reason: "stop_sequence"` bildirir ve tetiklenen sequence'i adlandırır: `/v1/messages` `stop_sequence` alanında, `/v1/complete` `stop` alanında. Yanıt kendiliğinden bittiğinde her iki alan da `null` kalır; böylece null kontrolü yapan bir istemci boş string ile yanıltılmaz. Önce `max_tokens` sınırına ulaşılırsa o kazanır ve bildirilen sebep `max_tokens` olur.

Akışlı bir yanıt sonradan değil, üretilirken kesilir. Bir sequence iki upstream chunk'ına bölünebilir; bu nedenle delta'lar, sequence'i tamamlayabilecek kuyruğu geride tutan bir writer'dan geçer ve karakter sınırında serbest bırakılır. Stop sequence göndermeyen bir istek hiçbir şeyi geride tutmaz ve her chunk'ı geldiği anda alır.

## MCP Sunucusu

`POST /mcp`, M365 Copilot'u Model Context Protocol istemcilerine JSON-RPC 2.0 üzerinden sunar (protokol sürümü `2025-06-18`). `initialize`, `tools/list`, `tools/call` ve `ping` desteklenir; lifecycle notification'ları gövdesiz `202` ile yanıtlanır. Bir API anahtarı yapılandırılmışsa bu route anahtar gerektirir.

| Tool | Argümanlar | Açıklama |
|------|------------|----------|
| `ask_copilot` | `prompt` (zorunlu), `model` | Metin döndüren tek, durumsuz Copilot turu |
| `describe_image` | `image_url` (zorunlu, data URI), `prompt` | Copilot'a satır içi bir görsel hakkında soru sorar |

```bash
curl -s -X POST http://localhost:8000/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_copilot","arguments":{"prompt":"CAP teoremini özetle"}}}'
```

Copilot, MCP rolünde bilinçli olarak leaf node'dur. `/v1` endpoint'lerinin kullandığı simulated tool calling MCP üzerinden **sunulmaz**: bir MCP istemcisinin zaten gerçek, şema ile zorlanan bir tool mekanizması vardır ve prompt tabanlı emülasyonu onun içine yerleştirmek birbiriyle yarışan iki tool loop'u oluşturur. Her MCP çağrısı, konuşma sürekliliği olmayan bağımsız bir turdur.

## Tool Calling (Araç Çağırma)

M365Bridge **simüle edilmiş tool calling** destekler — istemci tanımlı araçlar (Claude Code'un Read/Bash/Write'i, Codex araçları, vb.) M365 backend'inin bunları doğal olarak desteklemesi olmadan çalışır.

### Nasıl Çalışır?

1. İstemci, `tools` dizisi ile bir istek gönderir (OpenAI function tanımları veya Anthropic tool şemaları)
2. M365Bridge, tüm istek JSON'unu M365 Copilot'a gönderilen prompt'a gömer
3. M365 Copilot, ```` ```json ```` bloğunda tam yanıt JSON'u döndürür
4. M365Bridge, yanıtı ayrıştırır ve tool call'ları OpenAI `tool_calls` veya Anthropic `tool_use` içerik bloklarına çıkarır
5. İstemci aracı çalıştırır ve sonucu bir sonraki mesajda geri gönderir

Bu, hem OpenAI (`/v1/chat/completions`) hem de Anthropic (`/v1/messages`) endpoint'lerinde, hem streaming hem de non-streaming modlarda çalışır.

### Örnek (OpenAI)

```bash
curl http://127.0.0.1:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt5.5-reasoning",
    "messages": [{"role": "user", "content": "Run: echo hello"}],
    "tools": [{
      "type": "function",
      "function": {
        "name": "bash",
        "description": "Run a shell command",
        "parameters": {
          "type": "object",
          "properties": {"command": {"type": "string"}},
          "required": ["command"]
        }
      }
    }],
    "tool_choice": "required"
  }'
```

Yanıt:

```json
{
  "choices": [{
    "finish_reason": "tool_calls",
    "message": {
      "role": "assistant",
      "tool_calls": [{
        "id": "call_001",
        "type": "function",
        "function": {
          "name": "bash",
          "arguments": "{\"command\": \"echo hello\"}"
        }
      }]
    }
  }]
}
```

### Örnek (Anthropic)

```bash
curl http://127.0.0.1:8000/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt5.5-reasoning",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Run: echo hello"}],
    "tools": [{
      "name": "bash",
      "description": "Run a shell command",
      "input_schema": {
        "type": "object",
        "properties": {"command": {"type": "string"}},
        "required": ["command"]
      }
    }],
    "tool_choice": {"type": "any"}
  }'
```

Yanıt:

```json
{
  "content": [{
    "type": "tool_use",
    "id": "call_0e46d749-f182-419e-865f-abcb9c200de9",
    "name": "bash",
    "input": {"command": "echo hello"}
  }],
  "stop_reason": "tool_use"
}
```

### Notlar

- Tool calling her zaman etkindir — yapılandırma gerekmez. `tools` olmayan istekler etkilenmez.
- Tool call argümanları, tanımlanan JSON şemasına göre doğrulanır: `type`, `enum`, `required`, iç içe `properties` ve dizi `items`. Sözleşmeyi ihlal eden çağrı düşürülür ve proxy, ret gerekçesini taşıyan tek seferlik düzeltici bir yeniden-sorma yapar; böylece agent istemcileri çalıştırılamaz bir çağrı almaz. Bu, tek adımlı araç çağrılarında en iyi sonucu verir; çok turlu sürekli agent döngüleri (örneğin Claude Code'un `/init` komutu veya alt-agent görevleri) M365 backend modelinin kendi araç kullanım güvenilirliğine bağlıdır ve garanti edilmez.
- `additionalProperties: false` altında, şemanın tanımlamadığı argümanlar reddedilmek yerine silinir; böylece tek bir fazla alan bir tur maliyetine yol açmaz.
- `tool_choice` yalnızca prompt'ta istenmez, yanıt ayrıştırılırken zorlanır. `"none"` altında hiçbir çağrı iletilmez; belirli bir fonksiyon pinlendiğinde başka bir tool'a yapılan çağrı düşürülür ve yeniden sorulur.
- `parallel_tool_calls: false` (OpenAI, `/v1/chat/completions` ve `/v1/responses` üzerinde) ve `tool_choice.disable_parallel_tool_use: true` (Anthropic, `/v1/messages` üzerinde) aynı şekilde zorlanır: tur başına en fazla bir çağrı iletilir, kalanlar yeniden sıralanmak yerine düşürülür. Model çağrıları çalıştırılmasını istediği sırayla üretir ve bir sonraki tur sıradakini isteyebilir. Alan hiç verilmezse paralel çağrılara izin verilir; bu, her iki protokolün de varsayılanıdır.
- Her tool call id'si yeni bir `call_<uuid>` değeridir. Backend'in kendi id'leri turlar arasında tekrar eder ve istemciler bunları çift kayıt olarak reddeder.
- `tool_call_id` (OpenAI), `tool_use_id` (Anthropic) veya `call_id` (Responses) alanı eksik olan ya da aynı istekte hiç tanımlanmamış bir çağrıyı işaret eden tool sonucu HTTP 400 ile reddedilir. Hiç tool call tanımlamayan bir istek id kontrolünü atlar; böylece geçmişini kısaltmış bir istemci engellenmez.
- Backend, tool içeren bir isteği tool'ların var olmadığını söyleyen, işi kendi sandbox'ında çalıştırdığını iddia eden ya da çağıranın makinesine erişemediğini belirten düz metinle yanıtladığında, proxy açık bir talimatla bir kez yeniden sorar. İfadeler İngilizce, Çince ve Türkçe olarak tanınır. Sıradan bir metin yanıtı olduğu gibi geçer.
- M365 Copilot kendi sunucu tarafı araçlarını çalıştırdığında (web araması, code interpreter) ve simüle JSON yerine düz metin döndürdüğünde, yanıt normal bir metin tamamlaması olarak `finish_reason: "stop"` ile döndürülür.
- M365 kendi built-in'lerinden biri için tool call ürettiğinde (`search`, `code_interpreter`, `trigger_plugin`, `invoke_action`), o çağrı düşürülür ve tur `stop` ile biter. Bu, istek hiç tool tanımlamasa da geçerlidir: istemci o adları tanımlamadı ve çalıştıramaz, cevap zaten arama sonuçlarını içinde taşır.
- Backend ayrıştırılamayan bir tool calling envelope döndürdüğünde, envelope asistan mesajı olarak iletilmek yerine ayıklanır; tamamen envelope'tan ibaret bir yanıt kısa bir uyarı metnine dönüşür.
- M365 isteğin kendisini yanıtlamak yerine reddettiğinde, akışsız uç noktalar `upstream_content_blocked` ile HTTP 502 döndürür; böylece ret bir yanıt sanılmaz. Akışlı bir tur yanıtını çoktan açmış olduğu için yalnızca loglanır.
- Konuşma geçmişindeki `tool_result` mesajları (OpenAI) ve `tool_use`/`tool_result` içerik blokları (Anthropic), M365 backend'i tool rollerini anlamadığı için M365'ye gönderilmeden önce düz metne dönüştürülür.
- Streaming endpoint'leri, tool call'ları ayrıştırmadan önce tam yanıtı tampona alır (tool call JSON'u birden çok chunk'a yayılabilir). Tampon dolarken akış, bağlantı istemciye ölü görünmesin diye her on saniyelik sessizlikte bir keepalive çerçevesi yazar.

### İstemci Sürücülü Tool Loop'lar

Claude Code ve Codex gibi agent istemcileri tool loop'u kendileri sürer ve her istekte tüm çağrı ve sonuç geçmişini yeniden gönderir. Proxy bu istekler arasında durum tutmadığı için güncel kullanıcı turunun kanıtını gelen geçmişten yeniden kurar. Tur, tool result taşımayan son user mesajından başlar; böylece her sonucun user mesajı olarak geldiği Anthropic şekli yeni bir tur sanılmaz.

| Değişken               | Varsayılan | Açıklama                                                                     |
|------------------------|------------|------------------------------------------------------------------------------|
| `M365_MAX_TOOL_ROUNDS` | `32`       | Bir kullanıcı turunun sürebileceği tool round sayısı. Üst sınır `512`.        |
| `M365_ENABLE_WEB_SEARCH` | `1`      | Her turda M365 `BingWebSearch` built-in'ini tanımlar. `0`, `false`, `off` veya `no` bunu gönderilmez yapar. |

- Sınır aşıldığında `tool_round_limit` koduyla HTTP 409 döner ve round sayısı bildirilir. HTTP 409 Anthropic SDK'sının beklediği bir kod değildir, ama istemci bir tur daha isterken sonsuza kadar yanıt vermektense açık bir ret tercih edilir.
- Tamamlanmış çağrılar ve sonuçları prompt'ta kesin kanıt olarak tekrar edilir; böylece model elindeki sonucu yeniden istemek yerine ondan yanıt verir. Aynı çağrı aynı hatayla birden fazla kez başarısız olduysa prompt ayrıca yaklaşım değiştirmeyi ister.
- Sonucu turda zaten bulunan bir ad ve argüman ikilisini tekrarlayan tool call, üçüncü aynı denemede düşürülür. İlk tekrar geçer; çünkü yazma sonrası dosyayı okumak veya değişiklik sonrası testleri tekrar çalıştırmak olağandır. `tool_choice` ile talep edilen bir çağrı her zaman iletilir ve düşürme düzeltici re-ask'i tetiklemez; çünkü tekrar sorulduğunda model aynı çağrıyı üretir.
- Tekrar edilen her sonuç, kaldırılan bayt sayısını bildiren bir işaretin etrafında baş ve kuyruk olarak kırpılır; böylece uzun bir build logu döngünün her turunda prompt'u büyütmez.
- İki kez tanımlanan veya iki kez yanıtlanan bir tool call id'si HTTP 400 ile reddedilir: sonraki sonucun hangi çağrıya ait olduğu belirlenemez.
- Yalnızca hangi tool'u kullanacağını bildiren, tanımlı bir tool adını kısa ve fence'siz bir cümlede geçiren yanıt bir kez yeniden sorulur. Tekrar da bildirim olarak kalırsa cevap metni değiştirilir; böylece istemci hiç gelmeyecek bir çağrıyı beklemez.
- `function_call_progress` giriş item'ı, uzun süren bir istemci tool'unun ara durum bildirmesini sağlar. Modele bağlam olarak ulaşır ama bekleyen çağrıyı asla yanıtlamaz ve yeni bir kullanıcı turu başlatmaz.
- Grammar kısıtlı bir tool (`"type": "custom"`, örneğin Codex code mode'un `exec`'i) JSON argüman değil ham gövde alır. Backend bu gövdeyi fence'siz ürettiğinde, tek başına `{"input": "..."}` nesnesi olarak veya çıplak kaynak olarak, `/v1/responses` üzerinde kaçışlı metin yerine `custom_tool_call` olarak yakalanır.
- İstemcinin tanımladığı `web_search` tool'u istemciye asla yönlendirilmez: aramayı M365 kendi `BingWebSearch` built-in'i ile yapar ve sonuçları cevaba yazar. Yeteneğin var olduğunu model görsün diye tanım prompt'ta kalır. `web_search` tek tanımlı tool ise istek simüle tool yolundan tamamen çıkar ve düz metin olarak akar.
- İstek tool tanımlıyor, tur hiç tool call üretmiyor ve hiç tool result yoksa, işi birinci tekil şahısla yaptığını iddia eden yanıt hiçbir şeyin doğrulanmadığını söyleyen kısa bir cümleyle değiştirilir; özgün metin debug seviyesinde loglanır. "Go was created at Google" gibi üçüncü şahıs bir ifadeye ve uzun düz metin yanıtlara dokunulmaz. Değiştirme, tool tanımlı bir turu parse bitene kadar tamponlayan akışlı Chat Completions, Messages ve Completions uç noktalarında da geçerlidir. Yalnızca `/v1/responses` akışı içeriği çözdükçe yayımladığı için orada yalnızca loglanır.

## Built-in Coding Tools (Opt-in)

M365Bridge, sunucuda kısıtlı bir yerel coding işlemleri kümesi çalıştırabilir. Bu özellik **varsayılan olarak kapalıdır** ve ana gate `M365_ENABLE_CODE_TOOLS=1` ayarıdır. OpenAI Chat Completions (`/v1/chat/completions`), Anthropic Messages (`/v1/messages`) ve OpenAI Responses (`/v1/responses`) üzerinde kullanılabilir.

Özellik etkinleştirildiğinde, istekte açıkça bulunan araçlar tanınır ve yerel olarak çalıştırılır. `M365_AUTO_EXPOSE_TOOLS=1`, istemci araç sağlamadığında tüm built-in araçları isteğe otomatik olarak ekler; araçları istemcilerin açıkça seçmesi gerekiyorsa değeri `0` olarak bırakın. Sunucu, yerel sonuçları modele geri gönderir ve model nihai yanıt verene, istemci tanımlı bir tool call üretene veya iteration sınırına ulaşana kadar sürdürür. Tool call'ların ve ara sonuçların önce toplanması gerektiğinden, built-in araç kullanan istekler `stream: true` olsa bile model yanıtının tamamını buffer'a alır, ardından provider uyumlu streaming yanıtını yayınlar.

### Yapılandırma

| Değişken                        | Varsayılan | Açıklama                                                                                                   |
|---------------------------------|------------|------------------------------------------------------------------------------------------------------------|
| `M365_ENABLE_CODE_TOOLS`        | `0`        | Ana gate. Yerel araç çalıştırmayı etkinleştirmek için `1` yapın.                                           |
| `M365_AUTO_EXPOSE_TOOLS`        | `0`        | İstemci araç sağlamadığında tüm built-in tool şemalarını eklemek için `1` yapın.                           |
| `M365_WORKSPACE_DIR`            | `.`        | Dosya ve Git işlemlerini sınırlayan mevcut dizin.                                                          |
| `M365_CODE_TOOL_TIMEOUT`        | `30s`      | Her command veya test çalıştırması için timeout. `10s` ya da `2m` gibi Go duration sözdizimini kabul eder. |
| `M365_CODE_TOOL_MAX_OUTPUT`     | `1048576`  | Yakalanan command çıktısının byte cinsinden üst sınırı. Daha uzun çıktı kırpılır.                          |
| `M365_CODE_TOOL_MAX_READ_BYTES` | `1048576`  | Bir file read işleminin döndürebileceği azami byte sayısı.                                                 |
| `M365_CODE_TOOL_MAX_ITERATIONS` | `10`       | İstek başına model/tool loop iteration üst sınırı.                                                         |

Bu değişkenleri `data/.env` içine ekleyin. Docker kullanırken `M365_WORKSPACE_DIR`, container içinde zaten var olan bir dizini göstermelidir. Sağlanan Compose dosyası yalnızca `./data` dizinini `/app/data` konumuna mount eder; host kaynak workspace'ini açmaz.

### Kullanılabilir Araçlar

| Araç            | İşlem                                                                    |
|-----------------|--------------------------------------------------------------------------|
| `list_files`    | Workspace içindeki bir path altında bulunan dosya ve dizinleri listeler. |
| `read_file`     | Yapılandırılmış byte sınırına tabi olarak dosya okur.                    |
| `write_file`    | Workspace içinde dosya oluşturur veya mevcut dosyanın yerini alır.       |
| `search_files`  | Workspace dosyalarının içeriğinde arama yapar.                           |
| `git_status`    | Workspace Git durumunu gösterir.                                         |
| `git_diff`      | Workspace Git değişikliklerini gösterir.                                 |
| `git_log`       | Workspace içindeki yakın Git geçmişini gösterir.                         |
| `shell_command` | Workspace'i çalışma dizini olarak kullanıp shell command çalıştırır.     |
| `apply_patch`   | Workspace içinde unified patch uygular.                                  |
| `run_tests`     | Yapılandırılmış timeout ve output sınırıyla bir test command çalıştırır. |

### Güvenlik Gereksinimleri

Bu araçları etkinleştirmek, API'yi uzaktan kod ve dosya erişim yüzeyine dönüştürür. **Araçları etkinleştirmeden önce `M365_API_KEYS` veya `M365_API_KEY` yapılandırın; coding tools etkin olan her deployment için API key kimlik doğrulaması zorunludur.** Böyle bir deployment'ı doğrudan public internet'e açmayın. Least-privilege service account, ayrılmış workspace, sıkı dosya sistemi izinleri, network isolation ve container resource limitleri kullanın.

- **OWASP Broken Access Control:** eksik, sızmış veya paylaşılan bir API key, yetkisiz çağıranların mount edilen workspace'i okumasına, değiştirmesine veya burada komut çalıştırmasına izin verebilir. Benzersiz ve düzenli yenilenen key'ler kullanın; ayrıca güvenilir bir reverse proxy üzerinde authorization uygulayın.
- **Command Injection:** `shell_command` ve `run_tests`, modelin seçtiği command dizelerini çalıştırır. Prompt'ları, repo içeriğini, patch'leri ve tool argümanlarını güvenilmeyen girdi kabul edin; process'i izole edin ve production credential'ları vermeyin.
- **Path Traversal:** file tools, çözümlenen path'leri `M365_WORKSPACE_DIR` ile sınırlar; ancak gereğinden geniş bir workspace veya güvensiz mount yine de hassas dosyaları açığa çıkarır. Yalnızca gereken proje dizinini mount edin, symlink'leri ve izinleri inceleyin.
- **Sensitive Data Exposure:** tool çıktısı ve dosya içeriği çağırana döndürülebilir ve M365 backend'ine gönderilebilir. Secret'ları, token'ları, `.env` dosyalarını, SSH key'lerini, cloud credential'larını ve müşteri verilerini workspace dışında tutun.
- **Resource exhaustion:** command'ler, recursive aramalar, büyük dosyalar, output ve yinelenen tool loop'ları CPU, memory, disk ve process kapasitesi tüketebilir. Timeout, output, read ve iteration sınırlarını ölçülü tutun; container veya işletim sistemi quota'ları uygulayın.

## Responses API

`/v1/responses` uç noktası, OpenAI Responses API formatını uygular. `input` (string veya tipli öğe dizisi), `instructions`, `max_output_tokens`, `tools`, `reasoning` ve konuşma sürekliliği için `previous_response_id` kabul eder.

### Reasoning Effort

Codex CLI `reasoning: {"effort": ..., "summary": ...}` gönderir. Kabul edilen effort değerleri `none`, `minimal`, `low`, `medium`, `high`, `xhigh` ve `max`'dır; başka bir değer yok sayılmak yerine HTTP 400 ile reddedilir.

M365 ayrı bir effort ayarı sunmaz; bu yüzden effort, var olan tek kolu yönlendirir: `medium` ve üzeri, registry'de bir reasoning varyantı varsa isteği o varyanta yönlendirir, örneğin `gpt5.5` yerine `gpt5.5-reasoning`. Varyantı olmayan bir model veya zaten varyantı adlandıran bir anahtar değişmeden kalır. `summary` kabul edilir ama işlenmez.

### Custom Tool'lar

`"type": "custom"` ile tanımlanan bir tool, JSON argüman yerine serbest biçimli metin alır. Çağrıları, metin `input` alanında olacak şekilde `custom_tool_call` öğeleri olarak döner ve eşleşen `custom_tool_call` / `custom_tool_call_output` geçmiş öğeleri bir sonraki turda geri okunur.

### Örnek (akışsız)

```bash
curl http://127.0.0.1:8000/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt5.5",
    "input": "2+2 kaçtır?",
    "session_id": "my-session"
  }'
```

Yanıt:

```json
{
  "id": "resp_...",
  "object": "response",
  "created_at": 1234567890,
  "status": "completed",
  "model": "gpt-5.5",
  "output": [{
    "id": "msg_...",
    "type": "message",
    "status": "completed",
    "role": "assistant",
    "content": [{"type": "output_text", "text": "2+2 eşittir 4.", "annotations": []}]
  }],
  "output_text": "2+2 eşittir 4.",
  "usage": {"input_tokens": 5, "output_tokens": 8, "total_tokens": 13}
}
```

### Örnek (instructions ve input öğeleri ile)

```bash
curl http://127.0.0.1:8000/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt5.5-reasoning",
    "instructions": "Kısa ve öz bir asistansın.",
    "input": [{"role": "user", "content": [{"type": "input_text", "text": "Rekürsiyonu açıkla"}]}],
    "stream": true
  }'
```

### Streaming Olayları

Streaming uç noktası tipli SSE olayları yayınlar:

| Olay                                     | Açıklama                                                          |
|------------------------------------------|-------------------------------------------------------------------|
| `response.created`                       | Response nesnesi oluşturuldu (status: in_progress)                |
| `response.in_progress`                   | Response üretiliyor                                               |
| `response.output_item.added`             | Yeni output öğesi eklendi (message, reasoning veya function_call) |
| `response.content_part.added`            | İçerik parçası message öğesine eklendi                            |
| `response.output_text.delta`             | Metin deltası                                                     |
| `response.output_text.done`              | Metin tamamlandı                                                  |
| `response.content_part.done`             | İçerik parçası tamamlandı                                         |
| `response.output_item.done`              | Output öğesi tamamlandı                                           |
| `response.reasoning_summary_part.added`  | Reasoning parçası açıldı                                          |
| `response.reasoning_summary_text.delta`  | Reasoning/düşünme deltası                                         |
| `response.reasoning_summary_text.done`   | Reasoning tamamlandı                                              |
| `response.reasoning_summary_part.done`   | Reasoning parçası kapandı                                         |
| `response.function_call_arguments.delta` | Tool call argüman deltası                                         |
| `response.function_call_arguments.done`  | Tool call argümanları tamamlandı                                  |
| `response.completed`                     | Tam response nesnesi (status: completed)                          |
| `response.failed`                        | Hata oluştu (status: failed)                                      |

### Codex Uyumluluğu

Codex CLI, herhangi bir sohbet isteği göndermeden önce sağlayıcıyı iki probe ile açar.

- `GET /v1/health`, API key olmadan ve upstream'e hiç dokunmadan `{"status": "ok"}` döner. Burada 404 almak Codex'in tüm sağlayıcıyı erişilemez işaretlemesine yol açar.
- Girdisinde metin, görsel, tool call veya tool result taşımayan bir `POST /v1/responses` isteği, akışlı veya değil, yerel olarak boş ama geçerli bir Response ile yanıtlanır. Bu boş turu upstream'e göndermek yaklaşık on iki saniye ve conversation quota'sından bir mesaj harcıyordu. `instructions` taşıyan istek gerçek bir turdur ve M365'e ulaşmaya devam eder.

Her akışlı uç nokta ayrıca on saniyelik sessizlikten sonra bir keepalive çerçevesi yazar, çünkü tool tanımlı bir tur metnini tool call parse'ı bitene kadar tamponlar. OpenAI biçimli yollar hiçbir istemcinin veri olarak ayrıştırmadığı bir SSE yorumu gönderir; `/v1/messages` ve `/v1/complete` Anthropic `ping` olayını gönderir.

`/v1/chat/completions` ve `/v1/completions` bu yorumu ayrıca akış açılır açılmaz, upstream tur başlamadan önce yazar. Diğer her akışlı yol zaten önce bir çerçeve üretiyor (`message_start`, `ping` veya `response.created`); böylece istemci yavaş bir sağlayıcı ile ölü bir sağlayıcıyı ayırt etmek zorunda kalmaz.

İstemcisi gitmiş bir akışı iki kural daha korur. Her çerçeve otuz saniyelik bir yazma zaman sınırı kurar, böylece okumayı bırakan bir istemci handler'ı ve upstream WebSocket'i açık tutamaz. Başarısız bir keepalive yazımı veya iptal edilen bir istek context'i turu bitirir ve kapanmış bir sokete yazmak yerine upstream bağlantısını bırakır.

## Responses Compact API

`/v1/responses/compact` uç noktası, Codex uzaktan sıkıştırma için OpenAI Responses Compact API'yi uygular. `/v1/responses` ile aynı istek gövdesini kabul eder (model, input, instructions, tools, stream) ve tam olarak bir `compaction` output item içeren sıkıştırılmış bir response döndürür.

### Nasıl Çalışır

1. Konuşma geçmişi (input items) bir compaction prompt ile tek bir user mesajına düzleştirilir
2. Mesaj, özet üretmek için M365 Copilot'a gönderilir
3. Özet, `encrypted_content` alanına sahip bir `compaction` output item içinde döndürülür

### Örnek (akışsız)

```bash
curl http://127.0.0.1:8000/v1/responses/compact \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt5.5-reasoning",
    "input": [
      {"role": "user", "content": "sso.go içindeki auth bug'ını düzelt"},
      {"role": "assistant", "content": "Eksik sso_reload parametresini ekledim."},
      {"role": "user", "content": "Şimdi refresh yoluna logging ekle"}
    ]
  }'
```

Yanıt:

```json
{
  "id": "resp_...",
  "object": "response",
  "status": "completed",
  "output": [{
    "id": "cmp_...",
    "type": "compaction",
    "encrypted_content": "Konuşma bir SSO auth bug'ını düzeltmeye odaklandı..."
  }]
}
```

### Akışlı Mod

Akışlı mod, `/v1/responses` ile aynı SSE event dizisini yayar (`response.created`, `response.in_progress`, `response.output_item.added`, `response.output_item.done`, `response.completed`, `[DONE]`), ancak output item `type: "compaction"` olur.

### Notlar

- İstek gövdesinde özel `instructions` verilirse varsayılan compaction prompt'unun yerine geçer
- En iyi sonuç için compaction isteği yeni bir session ID kullanmalıdır (mevcut konuşmayı tekrar kullanmamalıdır)

## Proje Yapısı

```
cmd/cli/main.go            # Tek giriş noktası, alt komut yönlendirici
pkg/
  atomicfile/              # Yaz-ve-adlandır; çökme yarım yazılmış bir kimlik bilgisi bırakamaz
  auth/auth.go             # TokenManager, token yenileme, AES şifreli refresh token depolama
  auth/sso.go              # SSO cookie ile yeniden kimlik doğrulama ve designer broker token akışı
  client/client.go         # M365Client, istek başına tek SignalR WebSocket
  client/conversations.go  # ConversationClient: web sohbetlerini listeleme, yeniden adlandırma, silme
  client/history.go        # Bir sohbetin turlarını render edilmiş sayfasından okur
  client/citations.go      # Akan cevap metnindeki alıntı çözümlemesi
  client/errors.go         # UpstreamError; başarısız dial veya upload'ın durum kodunu taşır
  codingtools/             # Yerleşik yerel araçlar, M365_ENABLE_CODE_TOOLS ile açılır
  crypto/crypto.go         # Refresh token'lar için AES-256-GCM şifreleme
  logging/                 # Uygulama loglaması
  models/models.go         # Version, ModelRegistry, Config, LoadConfig, FindModel
  payload/payload.go       # İstek payload oluşturucuları, URL oluşturucu, locale/timezone yardımcıları
  servers/
    api.go                 # HTTP uyarlaması: her uç nokta, token sayımı, oturum izolasyonu
    cli.go                 # CLI sunucusu, etkileşimli mod
    errors.go              # Her route'un bildirdiği tek hata biçimi
    mcp.go                 # JSON-RPC 2.0 Model Context Protocol sunucusu
    sessions.go            # Oturumdan sohbete eşleme route'ları
    stopsequence.go        # Stop sequence kesme, akış yazıcısı dahil
    transcripts.go         # Mesaj içeriğinin diske ulaştığı tek yer
    webui.go               # Gömülü tarayıcı arayüzünü sunar
  setup/wizard.go          # Tarayıcı tabanlı kurulum sihirbazı (JS kod parçacığı, token doğrulama, data/.env kaydı)
  textcut/                 # Rune sınırına saygılı kesme
  toolcalling/             # Simüle edilmiş tool calling, ayrıştırıcıları ve dedektörleri
  webui/embed.go           # Derlenmiş arayüz, binary'ye gömülü
go.mod                     # Modül: github.com/KilimcininKorOglu/M365Bridge, Go 1.26
web/                       # Arayüzün Vite projesi; make ui bunu pkg/webui/dist'e derler
docs/                      # README dosyalarının kullandığı ekran görüntüleri
data/                      # Çalışma zamanı verisi (gitignore'lı): tokens/, setup.json, cache/, transcripts/
```

## Bağımlılıklar

Üç doğrudan bağımlılık ve onların getirdiği bir tanesi.

| Bağımlılık                      | Amaç                                                                  |
|---------------------------------|-----------------------------------------------------------------------|
| `github.com/google/uuid`        | SID'ler ve istek ID'leri için UUID oluşturma                          |
| `github.com/gorilla/websocket`  | SignalR için WebSocket istemcisi                                      |
| `github.com/pkoukk/tiktoken-go` | Kullanım ve max_tokens uygulaması için BPE token sayımı (o200k_base, yedek cl100k_base) |
| `github.com/dlclark/regexp2`    | Dolaylı; tiktoken-go'nun metni böldüğü regex motoru                   |

## Güvenlik

- Refresh token'lar depolamadan önce AES-256-GCM ile şifrelenir
- SSO ve M365 web cookie'leri depolamadan önce AES-256-GCM ile şifrelenir (`data/tokens/sso_cookies.json` ve `data/tokens/m365_cookies.json`)
- Eski plaintext M365 cookie depoları ilk kullanımda otomatik olarak şifrelenir
- Şifreleme anahtarı `data/tokens/encryption.key` dosyasında saklanır; anahtar kaybolursa şifreli kimlik bilgileri okunamaz ve setup wizard yeniden çalıştırılmalıdır
- Access token'lar `data/tokens/token_cache.json` dosyasında önbelleğe alınır (disk'te saklanır, ~1 saat geçerli, 60 saniye buffer ile)
- Arka plan token yenileyici, `serve` modunda her 30 dakikada bir access token'ı proaktif olarak yeniler
- SSO cookie otomatik yenileme, refresh token süresi dolduğunda (24h SPA limiti) sessizce yeniden kimlik doğrular
- Kod veya repoda kimlik bilgisi saklanmaz
- `data/` dizini gitignore'lıdır (token, önbellek, setup.json içerir)
- API anahtarı kimlik doğrulaması, yapılandırıldığında tüm `/v1/*` uç noktalarını korur
- Anahtar `Authorization: Bearer <key>` veya `x-api-key: <key>` başlığından okunur; istemci ikisini birden gönderdiğinde birinin geçerli olması yeterlidir

## Görsel Girdi Desteği

Proxy, OpenAI ve Anthropic API formatları ile çok modlu görsel girdiyi destekler:

- **OpenAI**: `{"type": "image_url", "image_url": {"url": "data:image/png;base64,..."}}` blokları içeren `content` dizisi
- **Responses**: url'i düz string olarak taşıyan `{"type": "input_image", "image_url": "data:image/png;base64,..."}` blokları içeren `content` dizisi
- **Anthropic**: `{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "..."}}` blokları içeren `content` dizisi

Bir `image_url` bloğu düz string biçimini de kabul eder, çünkü istemciler bu biçimi her iki blok adı altında da gönderir. `file_id` referansı desteklenmez; bu gateway bir Files API sunmaz.

Görseller, `POST https://substrate.office.com/m365Copilot/UploadFile` üzerinden M365 backend'ine yüklenir ve WebSocket mesajına `messageAnnotations` olarak eklenir. Desteklenen formatlar: PNG, JPEG, GIF, WebP.

### Uzak Görsel URL'leri

Bir OpenAI `image_url` bloğu, data URL yerine uzak bir `https://` adresi de taşıyabilir. Proxy bu adresi yüklemeden önce indirir.

İndirmede hiçbir kimlik bilgisi gönderilmez, bu yüzden her public https host kabul edilir. İstek yine de kontrol edilir; amaç proxy'nin kendi ağı içindeki adreslere ulaşmak için kullanılmasını engellemektir: düz http, loopback, private, link-local, multicast, carrier-grade NAT ve cloud metadata hedefleri reddedilir, host DNS çözümlemesinden sonra yeniden kontrol edilir. 20 MB'tan büyük, içerik tipi görsel olmayan veya tamamen başarısız olan bir yanıt, tüm isteği değil yalnızca o görseli düşürür. Tur başına en fazla 16 uzak görsel indirilir.

Anthropic `image` blokları base64 veriyi doğrudan taşır ve bu akıştan etkilenmez.

`input_file`, `file`, `input_audio` ve `audio` içerik blokları DEBUG log kaydıyla düşürülür, çünkü M365 backend'i yalnızca görsel ek kabul eder.

## Görsel Üretimi

Proxy, M365 Copilot'un Microsoft Designer görsel üretimini OpenAI Images API uç noktaları olarak sunar:

- `POST /v1/images/generations` (JSON body): Metin prompt'undan görsel üret (dosya yükleme yok)
- `POST /v1/images/edits` (multipart/form-data): Mevcut görsel(ler)i metin prompt'u ile düzenle; tekrarlanan `image` form alanları ile 16'ya kadar görsel desteklenir

Her iki uç nokta aşağıdaki parametreleri kabul eder:

| Parametre         | Tip    | Varsayılan  | Açıklama                                                                                  |
|-------------------|--------|-------------|-------------------------------------------------------------------------------------------|
| `prompt`          | string | (zorunlu)   | Görsel üretimi/düzenleme için metin prompt'u                                              |
| `n`               | int    | 1           | Üretilecek görsel sayısı (M365 her istek için bir tane üretir)                            |
| `size`            | string | `1024x1024` | Boyut ipucu (prompt'a doğal dil olarak eklenir; `1024x1024` atlanır)                      |
| `quality`         | string | `standard`  | Kalite ipucu (prompt'a eklenir; `standard` atlanır)                                       |
| `style`           | string | `natural`   | Stil ipucu (prompt'a eklenir; `natural` atlanır)                                          |
| `response_format` | string | `url`       | Yanıt formatı: `url` data URL (base64) döndürür, `b64_json` base64'ü ayrı alanda döndürür |
| `session_id`      | string | (opsiyonel) | Konuşma sürekliliği için session ID                                                       |
| `user`            | string | (opsiyonel) | `session_id` yoksa session ID olarak okunur                                               |

### Yanıt Formatı

- `response_format=url` (varsayılan): Görseli sunucu tarafında indirir ve `data:image/png;base64,...` data URL olarak döndürür. İndirme başarısız olursa raw `designerapp.officeapps.live.com` URL'ine düşer.
- `response_format=b64_json`: Görseli sunucu tarafında broker token kullanarak indirir ve base64 ile kodlanmış PNG verisi olarak `b64_json` alanında döndürür.

### Görsel Host Allowlist'i

Üretilen görsel URL'leri modelin kendi markdown çıktısından okunur, yani güvenilmez girdidir ve indirme designerapp access token'ını gönderir. Bu nedenle proxy yalnızca allowlist'teki host'lara bağlanır, `https` zorunlu tutar ve loopback, private, link-local, carrier-grade NAT veya cloud metadata adreslerine çözümlenen host'ları reddeder. Bu kontrolleri geçemeyen URL istemciye döndürülmez, tamamen düşürülür.

| Değişken | Varsayılan | Açıklama |
|----------|------------|----------|
| `M365_IMAGE_HOST_ALLOWLIST` | `.officeapps.live.com` | Üretilen görselleri sunabilecek host'lar (virgülle ayrılır). Nokta ile başlayan girdi o alan adını ve alt alan adlarını kapsar. |

### Görsel İndirme Token Akışı

Görsel üretildiğinde, proxy `designerappservice.officeapps.live.com` için MSAL.js broker token akışı ile bir JWE access token alır ve görseli indirir (`url` ve `b64_json` formatlarının ikisinde de):

1. Broker app (`c0ab8ce9`), M365 web app (`4765445b`) adına `designerappservice.officeapps.live.com/.default` scope'u ile token alır
2. Broker uyumlu refresh token `data/tokens/rt_broker.txt`'de (şifreli) saklanır, arka plan token yenileyici tarafından otomatik rotate edilir
3. Broker refresh token yoksa, SSO cookie broker authorize akışı ile alınır (PKCE + `brk-multihub://outlook.office.com` redirect URI)
4. JWE token ve `fileToken` header'ı ile görsel `designerapp.officeapps.live.com`'dan indirilir
5. İndirilen görsel base64 olarak kodlanır ve `b64_json` alanında döndürülür

### Örnek

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8230/v1",
    api_key="your-api-key",  # API anahtarı yoksa atla
)

resp = client.images.generate(
    model="gpt5.5-reasoning",
    prompt="gün batımında sakin bir dağ manzarası",
    n=1,
    response_format="b64_json",
)

# resp.data[0].b64_json, base64 ile kodlanmış PNG içerir
import base64
with open("output.png", "wb") as f:
    f.write(base64.b64decode(resp.data[0].b64_json))
```

## Uygulanmayan Özellikler

- Dosya yükleme
- Kod yorumlayıcı

## Sorumluluk Reddi

Bu proje yalnızca öğrenim ve araştırma amaçlıdır. Genel ağ iletişim protokollerini araştırır.

Bu projeyi kullanarak şunları onaylarsınız:
- Meşru Microsoft 365 Copilot yetkiniz olduğunu
- Kişisel öğrenim ve araştırma için olduğunu, ticari kullanım olmadığını
- Resmi olmayan arayüzler kullanmanın risklerini anladığınızı
- Tüm sonuçları kabul ettiğinizi

Bu proje şunları yapmaz:
- Şifreleme kırmaz veya kimlik doğrulamayı atlatmaz
- Başkalarının verisine erişmez veya sızdırmaz
- Microsoft servislerine müdahale etmez
- Microsoft Corporation ile hiçbir ilişkisi yoktur

## Lisans

Yalnızca Araştırma
