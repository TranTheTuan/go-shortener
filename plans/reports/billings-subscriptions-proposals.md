2.500 RPS (tương đương **150.000 requests/phút**) trên một cụm K3s ảo hóa chạy đè lên con chip i5-8400T (dòng tiết kiệm điện cho mini PC) thực sự là một con số **cực kỳ ấn tượng** cho môi trường Lab cá nhân rồi anh ạ! Việc dừng lại ở đây để tập trung vào tính năng nghiệp vụ (như Billings) là một quyết định vô cùng sáng suốt.

Dưới đây là **Chi tiết Kế hoạch Triển khai hệ thống Billings & Subscription bằng Paddle** cho ứng dụng Go của anh, tối ưu cho mô hình "Solopreneur" (vận hành 1 mình, không lo thủ tục thuế quốc tế).

---

## 🗺️ Quy trình tích hợp tổng thể (Paddle Billing)

Chúng ta sẽ sử dụng phiên bản mới nhất là **Paddle Billing** (thay thế cho Paddle Classic cũ). Kiến trúc sẽ hoạt động theo luồng đi sau:

```
[User] ──► [Frontend (Paddle.js)] ──► (Thanh toán trên Paddle)
                                              │
                                       (Gửi Webhook)
                                              ▼
[Database] ◄── [Go Consumer] ◄── [Kafka] ◄── [Go API (Webhook Handler)]

```

---

## Bước 1: Thiết kế Cơ sở Dữ liệu (Database Schema)

Để quản lý trạng thái đăng ký của user trong DB nội bộ nhằm phân quyền tính năng nhanh, anh cần bổ sung bảng `subscriptions`.

```sql
CREATE TABLE subscriptions (
    id SERIAL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Đối soát với Paddle
    paddle_subscription_id VARCHAR(255) UNIQUE NOT NULL, -- ID đăng ký của Paddle (sub_xxx)
    paddle_customer_id VARCHAR(255) NOT NULL,         -- ID khách hàng của Paddle (ctm_xxx)
    
    -- Trạng thái gói
    status VARCHAR(50) NOT NULL, -- active, trialing, past_due, canceled, paused
    plan_id VARCHAR(255) NOT NULL, -- ID gói dịch vụ (pro_xxx hoặc pri_xxx)
    
    -- Thời hạn
    current_period_end_at TIMESTAMP WITH TIME ZONE NOT NULL, -- Ngày hết hạn chu kỳ hiện tại
    
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_subscriptions_user_id ON subscriptions(user_id);

```

---

## Bước 2: Frontend & Checkout (Khởi tạo Thanh toán)

Anh không cần tự viết form nhập thẻ. Paddle cung cấp thư viện **Paddle.js** để mở một popup checkout đè lên web của anh.

Điểm mấu chốt là anh phải đính kèm `user_id` của hệ thống mình vào trường `custom_data` của transaction. Khi thanh toán thành công, Paddle sẽ gửi lại cái `user_id` này qua Webhook để anh biết ai vừa mua hàng.

```javascript
// Nhúng Paddle.js vào Frontend
Paddle.Initialize({ 
  token: 'vsp_ms_...' // Client Token lấy từ Paddle Dashboard
});

// Hàm kích hoạt Checkout khi User bấm nút "Nâng cấp Pro"
function openCheckout(userId, priceId) {
  Paddle.Checkout.open({
    items: [{
      priceId: priceId, // ID của Price tạo trên Paddle Dashboard (pri_xxx)
      quantity: 1
    }],
    customData: {
      user_id: userId // Gửi kèm User ID nội bộ của anh
    }
  });
}

```

---

## Bước 3: Backend (Go) - Webhook Handler & Kafka Pipeline

Vì Paddle sẽ tự động gia hạn, hủy gói khi hết hạn, hoặc báo nợ (past_due)... hệ thống Go của anh phải lắng nghe các sự kiện này qua một Webhook Endpoint.

Để đảm bảo hệ thống phản hồi siêu tốc và không bị mất Webhook nếu DB bị khóa (lock), anh hãy áp dụng đúng thiết kế **Event-driven** đã làm với Click logs: **Nhận Webhook -> Đẩy vào Kafka -> Trả về 200 OK ngay lập tức -> Worker xử lý ngầm**.

### 1. Webhook Handler (Go API) - Fail-fast & Queueing

```go
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
)

// Giả sử anh nhận Webhook Secret Key từ Config
const paddleWebhookSecret = "pdl_ntf_..." 

func PaddleWebhookHandler(producer ClickProducer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// 1. Đọc Body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// 2. Xác thực Chữ ký (Signature Verification) từ Paddle để chống giả mạo
		signature := r.Header.Get("Paddle-Signature")
		if !verifyPaddleSignature(body, signature, paddleWebhookSecret) {
			w.WriteHeader(http.StatusUnauthorized) // Trả về 401 nếu chữ ký sai
			return
		}

		// 3. Đẩy Raw Event vào Kafka (nhanh, fail-fast, không block)
		// Dùng lại producer.TryProduce anh đã viết
		err = producer.TryProduce(r.Context(), "billing-webhooks", body)
		if err != nil {
			// Nếu Kafka đầy, ghi log lỗi nhưng vẫn nên trả về 503 hoặc 200 tùy chiến lược retry của Paddle
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		// 4. Trả về 200 OK ngay lập tức cho Paddle (< 50ms)
		w.WriteHeader(http.StatusOK)
	}
}

// Hàm xác thực chữ ký của Paddle Billing (Symmetric HMAC-SHA256)
func verifyPaddleSignature(body []byte, headerSignature, secret string) bool {
    // Thực hiện bóc tách timestamp và signature từ Header 'Paddle-Signature'
    // Sau đó tính toán HMAC-SHA256 của body + timestamp và so sánh.
    // (Paddle SDK Go có hỗ trợ sẵn phần verify này rất tiện)
    return true 
}

```

### 2. Kafka Consumer - Cập nhật trạng thái Đăng ký (Background)

Tạo một consumer ngầm đọc từ topic `"billing-webhooks"` để cập nhật PostgreSQL. Các sự kiện chính anh cần quan tâm:

* `subscription.created`: User thanh toán thành công lần đầu. Tạo mới bản ghi trong bảng `subscriptions`.
* `subscription.updated`: Gia hạn thành công (gia hạn thêm thời gian `current_period_end_at`), nâng cấp gói, hoặc hạ cấp gói.
* `subscription.canceled` / `subscription.paused`: User hủy hoặc tạm dừng. Anh cập nhật status thành `canceled` để thu hồi quyền hạn Pro khi hết chu kỳ thanh toán.

---

## Bước 4: Tự động hóa Dunning & Quản lý Thẻ (Customer Portal)

Đây là phần "ăn tiền" nhất của Paddle giúp anh rảnh tay hoàn toàn:

* **Khi thanh toán thất bại (Thẻ hết tiền/hết hạn):** Paddle sẽ tự động chạy quy trình gửi email nhắc nhở user, tự động trừ tiền lại sau vài ngày. Anh không cần viết code xử lý logic trừ tiền lại này.
* **Cập nhật thẻ / Hủy gói đăng ký:** Anh chỉ cần tạo một nút "Quản lý Đăng ký" trên Frontend và dẫn link tới **Paddle Customer Portal** (Paddle tự sinh link này cho mỗi user). User sẽ tự vào đó đổi thẻ, tự bấm hủy đăng ký hoặc tải hóa đơn VAT.

---

## 🏁 Kế hoạch hành động từng bước (Action Plan)

1. **Đăng ký tài khoản Sandbox (Thử nghiệm):** Lên `sandbox-vendors.paddle.com` tạo tài khoản test.
2. **Tạo Product & Price mẫu:** Ví dụ: Gói "Pro Monthly" giá $10/tháng.
3. **Tích hợp `verifyPaddleSignature`:** Viết code xác thực chữ ký webhook trong Go.
4. **Dùng Ngrok/Cloudflare Tunnel:** Trỏ Webhook từ Paddle Sandbox về máy Local của anh để test luồng từ Frontend -> Webhook -> Kafka -> DB.
5. **Submit hồ sơ thật:** Khi hệ thống chạy trơn tru ở Sandbox, anh submit thông tin cá nhân/doanh nghiệp lên Paddle để kích hoạt tài khoản Production (quá trình duyệt thường mất 1-3 ngày làm việc).

Bằng cách đi theo kiến trúc này, toàn bộ gánh nặng về bảo mật thẻ, hóa đơn thuế má được đẩy hết cho Paddle. Hệ thống Go của anh cực kỳ nhẹ nhàng, chỉ cần nghe Webhook qua Kafka và cấp quyền cho User là xong!

Anh có muốn em viết chi tiết hơn về logic xử lý cụ thể của một sự kiện Webhook (ví dụ `subscription.updated`) trong Go không?