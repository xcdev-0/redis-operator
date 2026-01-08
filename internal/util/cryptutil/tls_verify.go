package cryptutil

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"
)

// VerifyCertificateExceptServerName는 TLS 인증서 검증을 수행하되,
// 서버 이름(호스트명) 검증은 건너뜁니다. 서버 호스트명과 일치하지 않아도
// 신뢰할 수 있는 CA에 의해 서명된 유효한 인증서를 검증할 수 있습니다.
//
// 매개변수:
//   - rawCerts: 피어(서버)로부터 받은 원시 인증서 데이터 (ASN.1 DER 인코딩)
//     TLS 표준에 따라 인증서 체인 순서:
//   - rawCerts[0]: 리프(Leaf) 인증서 (서버의 실제 인증서)
//   - rawCerts[1]: 첫 번째 중간(Intermediate) CA 인증서
//   - rawCerts[2..n]: 추가 중간 CA 인증서들 (있는 경우)
//     주의: 루트(Root) CA 인증서는 rawCerts에 포함되지 않음
//     (클라이언트가 이미 보유하고 있는 신뢰 저장소에서 찾음)
//   - config: TLS 설정 정보 (루트 CA, 검증 시간 등 포함)
//
// 반환값:
//   - []*x509.Certificate: 파싱된 인증서 체인 (leaf부터 intermediate까지)
//   - [][]*x509.Certificate: 검증된 인증서 체인들 (leaf에서 root까지의 경로들)
//   - error: 검증 실패 또는 인증서 파싱 실패 시 에러
func VerifyCertificateExceptServerName(rawCerts [][]byte, config *tls.Config) ([]*x509.Certificate, [][]*x509.Certificate, error) {
	// 1단계 검증: 피어로부터 인증서가 제공되었는지 확인
	// 인증서가 없으면 검증할 대상이 없으므로 조기 반환
	if len(rawCerts) == 0 {
		return nil, nil, errors.New("tls: no certificates provided by peer")
	}

	// 2단계 검증: TLS 설정이 nil이 아닌지 확인
	// 설정이 없으면 검증을 수행할 수 없음
	if config == nil {
		return nil, nil, errors.New("tls: config cannot be nil")
	}

	// 3단계 검증: 루트 CA가 설정되어 있는지 확인
	// 루트 CA가 없으면 인증서 체인을 검증할 수 없음
	if config.RootCAs == nil {
		return nil, nil, errors.New("tls: no root CAs configured for verification")
	}

	// 인증서 체인의 모든 인증서를 파싱
	// rawCerts는 바이트 배열(ASN.1 DER 형식)이므로 x509.Certificate 구조체로 변환 필요
	// TLS 표준: rawCerts[0]은 리프 인증서, rawCerts[1..]는 중간 인증서들
	certs := make([]*x509.Certificate, len(rawCerts))
	for i, asn1Data := range rawCerts {
		cert, err := x509.ParseCertificate(asn1Data)
		if err != nil {
			return nil, nil, fmt.Errorf("tls: failed to parse certificate at index %d: %w", i, err)
		}
		certs[i] = cert
	}

	// 검증 기준 시점 설정
	// config에 Time 함수가 설정되어 있으면 해당 시간 사용, 없으면 현재 시간 사용
	var verifyTime time.Time
	if config.Time != nil {
		verifyTime = config.Time()
	} else {
		verifyTime = time.Now()
	}

	// 중간 인증서 풀 구성
	// 인증서 체인에서 leaf(0번 인덱스)를 제외한 나머지 중간 인증서들을 풀에 추가
	// 중간 인증서들이 필요한 이유: 리프 인증서의 서명을 검증하려면 중간 CA의 공개키가 필요
	intermediates := x509.NewCertPool()
	for i := 1; i < len(certs); i++ {
		intermediates.AddCert(certs[i])
	}

	// 서버 이름 검증을 제외한 검증 옵션 설정
	// 표준 TLS 검증과의 차이점: DNSName 필드를 명시적으로 생략하여 호스트명 검증 스킵
	opts := x509.VerifyOptions{
		Roots:         config.RootCAs, // 신뢰할 수 있는 루트 CA 풀 (최종 신뢰 앵커)
		Intermediates: intermediates,  // 중간 인증서 풀 (리프->루트 경로 구축에 필요)
		CurrentTime:   verifyTime,     // 검증 기준 시점
		// DNSName 필드를 의도적으로 생략하여 호스트명 검증을 건너뜀
		// 이를 통해 서버 호스트명과 일치하지 않아도 유효한 인증서를 검증 가능
	}

	// 리프 인증서(체인의 첫 번째 인증서) 검증
	// 왜 리프 인증서만 검증하는데 루트 CA와 중간 인증서를 옵션으로 주는가?
	// → 리프 인증서 자체만으로는 검증 불가능. 신뢰 체인(Trust Chain) 검증이 필요:
	//   1) 리프 인증서의 서명을 중간 CA의 공개키로 검증
	//   2) 중간 CA 인증서의 서명을 루트 CA의 공개키로 검증
	//   3) 루트 CA가 신뢰 저장소(Roots)에 있는지 확인
	//   이 과정을 통해 리프->중간->루트까지의 신뢰 체인을 검증
	leafCert := certs[0]
	chains, err := leafCert.Verify(opts)
	if err != nil {
		return nil, nil, fmt.Errorf("tls: certificate verification failed: %w", err)
	}

	return certs, chains, nil
}
