# build-and-load.sh
#!/bin/bash
set -e  # 에러 발생시 중단

kubectl delete rediscluster test-redis-cluster | 2>/dev/null || true
kubectl delete -k config/default | 2>/dev/null || true

IMAGE_NAME="redis-operator:latest"

echo "🗑️  로컬 Docker의 기존 이미지 삭제 중..."
docker images | grep redis-operator | awk '{print $3}' | xargs docker rmi -f 2>/dev/null || true

echo "🔨 새 이미지 빌드 중..."
docker build -t $IMAGE_NAME .

echo "🗑️  미니쿠베 내부의 기존 이미지 삭제 중..."
if ! minikube ssh "docker images | grep redis-operator | awk '{print \$3}' | xargs -r docker rmi -f" 2>&1; then
    echo "⚠️  미니쿠베 이미지 삭제 실패 (이미지가 없거나 에러 발생, 무시하고 계속)"
fi

echo "📦 미니쿠베에 이미지 로드 중..."
minikube image load $IMAGE_NAME

echo ""
echo "✅ 완료! 이미지 확인:"
echo ""
echo "📦 로컬 Docker 이미지:"
LOCAL_IMAGE_ID=$(docker images redis-operator:latest --format "{{.ID}}" 2>/dev/null || echo "")
LOCAL_BUILD_TIME=$(docker inspect redis-operator:latest --format='{{.Created}}' 2>/dev/null || echo "")
docker images | grep redis-operator
echo ""
echo "🎯 미니쿠베 이미지:"
MINIKUBE_IMAGE_ID=$(minikube ssh "docker images redis-operator:latest --format '{{.ID}}'" 2>/dev/null || echo "")
MINIKUBE_BUILD_TIME=$(minikube ssh "docker inspect redis-operator:latest --format='{{.Created}}'" 2>/dev/null || echo "")
minikube image ls | grep redis-operator
echo ""
echo "🔍 이미지 비교:"
echo "  로컬 ID:          $LOCAL_IMAGE_ID"
echo "  로컬 빌드 시간:   $LOCAL_BUILD_TIME"
echo "  미니쿠베 ID:      $MINIKUBE_IMAGE_ID"
echo "  미니쿠베 빌드 시간: $MINIKUBE_BUILD_TIME"
echo ""
