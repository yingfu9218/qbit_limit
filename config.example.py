# 项目配置参数
class Config:
    # qbittorrent账号信息
    host = "127.0.0.1:8080"
    username = "admin"
    password = "adminadmin"

    # 上行速度和流量限制配置

    # 每天最大上行流量限制 ,如 20G = 20*1024*1024*1024
    uploadLimitEveryDay = 20 * 1024 * 1024 * 1024
    # 每月最大上行流量限制 如：700G= 600 * 1024 * 1024 * 1024
    uploadLimitEveryMonth = 600 * 1024 * 1024 * 1024

    # 上行速率限制,如 1m= 1 * 1024 * 1024
    uploadSpeedLimit = 1 * 1024 * 1024


    def __init__(self):
        pass
