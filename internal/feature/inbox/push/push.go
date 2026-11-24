package push

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
)

// Subscription 是一台裝置的 Web Push 訂閱端點。
type Subscription struct {
	Endpoint string `toml:"endpoint"`
	P256dh   string `toml:"p256dh"`
	Auth     string `toml:"auth"`
}

// Service 管這個家的推播：送出（Send）、公開金鑰（PublicKey）、訂閱端點（Subscriptions／Subscribe／Unsubscribe）。
// 金鑰每次送才讀秘密檔，不快取；訂閱檔的讀改寫由內部的鎖保護。
type Service struct {
	mu          sync.Mutex
	dir         string
	secretsPath func() string
}

// Option 調整 Service。
type Option func(*Service)

// WithSecretsPath 指定怎麼找秘密檔（預設每次讀家裡 home.toml 的 [secrets]）；回空字串表示沒設定。
func WithSecretsPath(f func() string) Option { return func(s *Service) { s.secretsPath = f } }

// New 建一個管 dir 這個家的推播通道。不做事、不開 goroutine、不讀任何檔。
func New(dir string, opts ...Option) *Service {
	s := &Service{dir: dir}
	s.secretsPath = s.homeSecretsPath
	for _, o := range opts {
		o(s)
	}
	return s
}

// SetHome 換成管 dir 這個家（inbox 塊的組裝先用空的家建好通道交給 notify，registry 叫 SetHome 時才知道家在哪；在用之前呼叫）。
func (s *Service) SetHome(dir string) { s.mu.Lock(); defer s.mu.Unlock(); s.dir = dir }

// homeSecretsPath 每次重讀 home.toml，所以改了設定下一次就生效。
func (s *Service) homeSecretsPath() string {
	h, err := platform.LoadHome(s.dir)
	if err != nil {
		return ""
	}
	return h.SecretsPath()
}

func (s *Service) file() string { return filepath.Join(s.dir, "settings", "push.toml") }

// Subscriptions 回目前所有訂閱端點（檔案不存在回空）。
func (s *Service) Subscriptions() ([]Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *Service) read() ([]Subscription, error) {
	if s.dir == "" { // 還沒 SetHome：當成沒有訂閱，不讀工作目錄
		return nil, nil
	}
	data, err := os.ReadFile(s.file())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, platform.Unavailable("無法讀取推播訂閱")
	}
	var cfg struct {
		Subscriptions []Subscription `toml:"subscription"`
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, platform.Invalid("subscription", "推播訂閱格式不正確")
	}
	for _, sub := range cfg.Subscriptions {
		if sub.Endpoint == "" || sub.P256dh == "" || sub.Auth == "" {
			return nil, platform.Invalid("subscription", "推播訂閱缺少必要欄位")
		}
	}
	return cfg.Subscriptions, nil
}

// base64URL 是推播金鑰（p256dh、auth）允許的字元：base64url，可帶結尾的 =。
var base64URL = regexp.MustCompile(`^[A-Za-z0-9_-]+=*$`)

// Validate 檢查一筆訂閱的格式：網址要 https://、兩把金鑰要是 base64url。錯誤帶欄位名，不帶值。
func Validate(sub Subscription) error {
	if !strings.HasPrefix(sub.Endpoint, "https://") {
		return platform.Invalid("endpoint", "推播網址必須以 https:// 開頭")
	}
	if !base64URL.MatchString(sub.P256dh) {
		return platform.Invalid("p256dh", "推播金鑰格式不正確")
	}
	if !base64URL.MatchString(sub.Auth) {
		return platform.Invalid("auth", "推播驗證資料格式不正確")
	}
	return nil
}

// Subscribe 照 endpoint 新增或更新這台裝置的訂閱，回目前的訂閱數。格式不對回 Invalid，不寫檔。
func (s *Service) Subscribe(sub Subscription) (int, error) {
	if err := Validate(sub); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	subs, err := s.read()
	if err != nil {
		return 0, err
	}
	found := false
	for i := range subs {
		if subs[i].Endpoint == sub.Endpoint {
			subs[i], found = sub, true
		}
	}
	if !found {
		subs = append(subs, sub)
	}
	if err := s.write(subs); err != nil {
		return 0, err
	}
	return len(subs), nil
}

// Unsubscribe 照 endpoint 刪掉訂閱，回剩下幾筆；沒有這筆也算成功（不寫檔）。刪到 0 筆就寫空檔（不刪檔）。
func (s *Service) Unsubscribe(endpoint string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs, err := s.read()
	if err != nil {
		return 0, err
	}
	kept := make([]Subscription, 0, len(subs))
	for _, sub := range subs {
		if sub.Endpoint != endpoint {
			kept = append(kept, sub)
		}
	}
	if len(kept) == len(subs) {
		return len(subs), nil
	}
	if err := s.write(kept); err != nil {
		return 0, err
	}
	return len(kept), nil
}

// write 整檔重寫 settings/push.toml（原子寫入、0600）；呼叫者要持有 s.mu。
func (s *Service) write(subs []Subscription) error {
	if s.dir == "" { // 還沒 SetHome：不寫到工作目錄
		return platform.Unavailable("推播還沒接上家")
	}
	var out bytes.Buffer
	if len(subs) > 0 {
		if err := toml.NewEncoder(&out).Encode(map[string][]Subscription{"subscription": subs}); err != nil {
			return platform.Internal("無法編碼推播訂閱")
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.file()), 0o700); err != nil {
		return platform.Unavailable("無法建立推播設定目錄")
	}
	if err := platform.AtomicWrite(s.file(), out.Bytes(), 0o600); err != nil {
		return platform.Unavailable("無法儲存推播訂閱")
	}
	return nil
}
