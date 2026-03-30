
  1. leaf -> intermediate -> root 체인에서 클라이언트가 뭘 가져야 하나

  - 보통 클라이언트는 **루트 CA(또는 신뢰 번들)**만 신뢰 저장소에 있으면 됩니다.
  - 단, 이 전제가 성립하려면 서버가 핸드셰이크 때 leaf + intermediate를 같이 보내야 합니다.
  - 서버가 intermediate를 안 보내면, 클라이언트 쪽에 intermediate도 있어야 검증이 됩니다.

  2. Redis TLS에서 누가 인증서를 보내나

  - 기본 TLS(지금 설정): 서버(Redis) -> 클라이언트(redis-cli) 인증서 제공
  - mTLS일 때만: 서버가 클라이언트 인증서도 요구하고, 클라이언트 -> 서버 인증서 제공

  3. 지금 환경에서 왜 --cacert만으로 되는가

  - tls-auth-clients optional이라 클라이언트 인증서가 필수가 아닙니다.
  - 그래서 아래처럼 서버 검증만 하고 접속 가능:

    redis-cli --tls --cacert /tls/ca.crt ...

  4. 웹 브라우저와 비교

  - 원리는 동일합니다. 서버가 인증서 체인을 주고, 클라이언트(브라우저/redis-cli)가 신뢰 CA로 검증합니
    다.
  - 차이는 mTLS 사용 여부입니다. 웹은 보통 서버 인증만, mTLS는 양방향 인증입니다.

  추가로, 지금은 self-signed 1장 구조라 ca.crt == tls.crt인 상태라 더 단순하게 동작합니다.
