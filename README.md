## 项目介绍

`minfo` 是一个本地媒体信息检测 Web 工具，主要功能：
- 输出 MediaInfo 信息
- 输出 BDInfo 信息
- 使用 Seedbox 截图脚本生成 4 张截图压缩包
- 直接输出 Pixhost 图床链接

## Docker 运行

示例 `docker-compose.yml`：

```yaml
services:
  minfo:
    image: ghcr.io/mirrorb/minfo:latest
    container_name: minfo
    privileged: true
    ports:
      - "28081:8080"
    environment:
      PORT: "8080"
      WEB_PASSWORD: "adminadmin"
      REQUEST_TIMEOUT: "20m"
    volumes:
      - /lib/modules:/lib/modules:ro # 程序会自动尝试加载 `udf` 内核模块用于挂载ISO
      - /your/media/path1:/media_path1:ro
      - /your/media/path2:/media_path2:ro
      - /your/media/path3:/media_path3:ro
      - /your/media/path4:/media_path4:ro
    restart: unless-stopped
```

启动：

```bash
docker compose up -d
```

访问：
- `http://localhost:28081`
