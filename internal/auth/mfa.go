package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"forgeflow/internal/apperror"
)

const (
	mfaEnrollmentTTL  = 10 * time.Minute
	totpPeriodSeconds = int64(30)
	recoveryCodeCount = 10
)

var mfaBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

type MFAStatus struct {
	Enabled          bool       `json:"enabled"`
	Required         bool       `json:"required"`
	PendingExpiresAt *time.Time `json:"pendingExpiresAt,omitempty"`
}

type MFAEnrollment struct {
	Secret          string    `json:"secret"`
	ProvisioningURI string    `json:"provisioningUri"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type MFAConfirmation struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

func (s *Service) MFAStatus(ctx context.Context, userID string) (MFAStatus, error) {
	credential, err := s.store.FindUserByID(ctx, userID)
	if err != nil {
		return MFAStatus{}, err
	}
	if credential.Role != RoleAdmin {
		return MFAStatus{}, apperror.New(apperror.CodeForbidden, "administrator role is required")
	}
	return MFAStatus{Enabled: credential.MFAEnabledAt != nil, Required: s.options.AdminMFARequired || credential.MFAEnabledAt != nil, PendingExpiresAt: credential.MFAPendingExpiresAt}, nil
}

func (s *Service) BeginMFAEnrollment(ctx context.Context, userID, password string) (MFAEnrollment, error) {
	if len(s.options.MFAEncryptionKey) != 32 {
		return MFAEnrollment{}, apperror.New(apperror.CodeTransient, "MFA encryption key is unavailable")
	}
	if err := s.VerifyCurrentPassword(ctx, userID, password); err != nil {
		return MFAEnrollment{}, err
	}
	credential, err := s.store.FindUserByID(ctx, userID)
	if err != nil {
		return MFAEnrollment{}, err
	}
	if credential.Role != RoleAdmin {
		return MFAEnrollment{}, apperror.New(apperror.CodeForbidden, "administrator role is required")
	}
	if credential.MFAEnabledAt != nil {
		return MFAEnrollment{}, apperror.New(apperror.CodeConflict, "MFA is already enabled")
	}
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return MFAEnrollment{}, fmt.Errorf("generate MFA secret: %w", err)
	}
	ciphertext, err := sealMFASecret(s.options.MFAEncryptionKey, userID, secret)
	if err != nil {
		return MFAEnrollment{}, err
	}
	expiresAt := s.options.Now().Add(mfaEnrollmentTTL)
	if err := s.store.SetMFAPending(ctx, userID, ciphertext, expiresAt); err != nil {
		return MFAEnrollment{}, err
	}
	encoded := mfaBase32.EncodeToString(secret)
	label := url.PathEscape("ForgeFlow:" + credential.Email)
	query := url.Values{"secret": {encoded}, "issuer": {"ForgeFlow"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return MFAEnrollment{Secret: encoded, ProvisioningURI: "otpauth://totp/" + label + "?" + query.Encode(), ExpiresAt: expiresAt}, nil
}

func (s *Service) ConfirmMFAEnrollment(ctx context.Context, userID, sessionID, code string) (MFAConfirmation, error) {
	credential, err := s.store.FindUserByID(ctx, userID)
	if err != nil {
		return MFAConfirmation{}, err
	}
	if credential.Role != RoleAdmin || credential.MFAEnabledAt != nil || len(credential.MFAPendingSecretCiphertext) == 0 || credential.MFAPendingExpiresAt == nil || !s.options.Now().Before(*credential.MFAPendingExpiresAt) {
		return MFAConfirmation{}, apperror.New(apperror.CodeConflict, "MFA enrollment is unavailable or expired")
	}
	secret, err := openMFASecret(s.options.MFAEncryptionKey, userID, credential.MFAPendingSecretCiphertext)
	if err != nil {
		return MFAConfirmation{}, err
	}
	step, ok := matchingTOTPStep(secret, code, s.options.Now())
	if !ok {
		return MFAConfirmation{}, apperror.New(apperror.CodeUnauthorized, "MFA code is incorrect")
	}
	codes, hashes, err := generateRecoveryCodes(s.options.MFAEncryptionKey)
	if err != nil {
		return MFAConfirmation{}, err
	}
	if err := s.store.EnableMFA(ctx, userID, sessionID, credential.MFAPendingSecretCiphertext, hashes, step, s.options.Now()); err != nil {
		if err == ErrNotFound {
			return MFAConfirmation{}, apperror.New(apperror.CodeConflict, "MFA enrollment changed or expired")
		}
		return MFAConfirmation{}, err
	}
	return MFAConfirmation{RecoveryCodes: codes}, nil
}

func (s *Service) verifyAdminSecondFactor(ctx context.Context, credential UserCredential, factor string, now time.Time) (bool, error) {
	secret, err := openMFASecret(s.options.MFAEncryptionKey, credential.ID, credential.MFASecretCiphertext)
	if err != nil {
		return false, err
	}
	if step, ok := matchingTOTPStep(secret, factor, now); ok {
		return s.store.UseMFATimestep(ctx, credential.ID, step)
	}
	normalized := normalizeRecoveryCode(factor)
	if len(normalized) != 16 {
		return false, nil
	}
	return s.store.ConsumeMFARecoveryCode(ctx, credential.ID, recoveryCodeHash(s.options.MFAEncryptionKey, normalized))
}

func sealMFASecret(key []byte, userID string, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize MFA cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize MFA GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate MFA nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, []byte(userID)), nil
}

func openMFASecret(key []byte, userID string, ciphertext []byte) ([]byte, error) {
	if len(key) != 32 || len(ciphertext) == 0 {
		return nil, apperror.New(apperror.CodeTransient, "MFA secret is unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize MFA cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize MFA GCM: %w", err)
	}
	if len(ciphertext) <= gcm.NonceSize() {
		return nil, apperror.New(apperror.CodeInternal, "MFA secret ciphertext is invalid")
	}
	plaintext, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], []byte(userID))
	if err != nil {
		return nil, apperror.New(apperror.CodeInternal, "MFA secret cannot be decrypted")
	}
	return plaintext, nil
}

func matchingTOTPStep(secret []byte, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return 0, false
	}
	current := now.Unix() / totpPeriodSeconds
	for _, step := range []int64{current, current - 1, current + 1} {
		if step >= 0 && subtle.ConstantTimeCompare([]byte(totpCode(secret, step, 6)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

func totpCode(secret []byte, step int64, digits int) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	modulus := uint32(1)
	for range digits {
		modulus *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%modulus)
}

func generateRecoveryCodes(key []byte) ([]string, []string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	hashes := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, fmt.Errorf("generate MFA recovery code: %w", err)
		}
		normalized := mfaBase32.EncodeToString(raw)
		codes = append(codes, strings.Join([]string{normalized[0:4], normalized[4:8], normalized[8:12], normalized[12:16]}, "-"))
		hashes = append(hashes, recoveryCodeHash(key, normalized))
	}
	return codes, hashes, nil
}

func normalizeRecoveryCode(value string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(value)))
}

func recoveryCodeHash(key []byte, normalized string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("forgeflow:mfa-recovery:" + normalized))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
