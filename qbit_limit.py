import os
import sqlite3
from qbittorrentapi import Client
from datetime import datetime, timedelta
import schedule
import time
from loguru import logger
from config import Config
import humanize
from flask import Flask, render_template_string
import threading


c = Config

# 账号信息
host = c.host
username = c.username
password = c.password

# 每天最大上行流量限制
uploadLimitEveryDay = c.uploadLimitEveryDay

# 每月最大上行流量限制
uploadLimitEveryMonth = c.uploadLimitEveryMonth

# 上行速率限制
uploadSpeedLimit = c.uploadSpeedLimit

# 上行速率限制，相当于禁止上行  ，如5kb= 5 * 1024
denyUploadSpeedLimit = 5 * 1024

dbPath = os.path.dirname(os.path.abspath(__file__))
conn = sqlite3.connect(dbPath + '/qb.db', check_same_thread=False)
c = conn.cursor()

# Flask应用
app = Flask(__name__)

# HTML模板
HTML_TEMPLATE = """
<!DOCTYPE html>
<html>
<head>
    <title>流量统计查看</title>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        table { border-collapse: collapse; width: 100%; }
        th, td { border: 1px solid #ddd; padding: 12px; text-align: left; }
        th { background-color: #f2f2f2; }
        tr:nth-child(even) { background-color: #f9f9f9; }
        .summary { margin: 20px 0; padding: 15px; background-color: #e7f3ff; border-radius: 5px; }
        .summary h3 { margin-top: 0; }
        h1 { color: #333; }
        
        /* 移动端适配 */
        @media screen and (max-width: 768px) {
            body { margin: 10px; font-size: 14px; }
            h1 { font-size: 1.5em; }
            .summary { padding: 10px; }
            
            /* 表格响应式设计 */
            table, thead, tbody, th, td, tr { display: block; }
            thead tr { position: absolute; top: -9999px; left: -9999px; }
            
            tr { 
                border: 1px solid #ccc; 
                margin-bottom: 10px; 
                padding: 10px;
                background-color: #fff;
                border-radius: 5px;
            }
            
            td { 
                border: none;
                position: relative;
                padding: 8px 8px 8px 50%;
                text-align: left;
            }
            
            td:before { 
                content: attr(data-label) ": ";
                position: absolute;
                left: 6px;
                width: 45%;
                text-align: left;
                font-weight: bold;
                color: #333;
            }
        }
    </style>
    <script>
        setTimeout(() => location.reload(), 10000);
    </script>
</head>
<body>
    <h1>流量统计查看</h1>
    
    <div class="summary">
        <h3>当月总流量统计</h3>
        <p><strong>当月总上行流量：</strong> {{ month_upload }}</p>
        <p><strong>当月总下行流量：</strong> {{ month_download }}</p>
    </div>
    
    <h3>最近30天流量记录</h3>
    <table>
        <thead>
            <tr>
                <th>日期</th>
                <th>上传</th>
                <th>下载</th>
                <th>是否触发限速</th>
                <th>创建时间</th>
                <th>更新时间</th>
            </tr>
        </thead>
        <tbody>
            {% for row in logs %}
            <tr>
                <td data-label="日期">{{ row[0] }}</td>
                <td data-label="上传">{{ row[1] }}</td>
                <td data-label="下载">{{ row[2] }}</td>
                <td data-label="是否触发限速">{{ row[3] }}</td>
                <td data-label="创建时间">{{ row[4] }}</td>
                <td data-label="更新时间">{{ row[5] }}</td>
            </tr>
            {% endfor %}
        </tbody>
    </table>
</body>
</html>
"""




def getClient():
    return Client(host=host, username=username, password=password)


# 设置限速
def setUploadLimit():
    client = getClient()
    client.transfer.set_upload_limit(uploadSpeedLimit)


# 限制上传（禁止上传）up_rate_limit 获取qb到当前最大上行限速
def denyUpload(up_rate_limit):
    if denyUploadSpeedLimit != up_rate_limit:
        client = getClient()
        client.transfer.set_upload_limit(denyUploadSpeedLimit)


# 刷新qbit 最新数据，并更新到log表
def updateLog():
    today = datetime.now().strftime("%Y-%m-%d")
    now = datetime.now().strftime("%Y-%m-%d %H:%M:%S")

    client = getClient()
    transfer_info = client.transfer.info
    total_upload = transfer_info.get("up_info_data")
    total_download = transfer_info.get("dl_info_data")
    up_rate_limit = transfer_info.get("up_rate_limit")
    upload = 0
    down = 0
    deny_limit = 0
    # 判断是否当月超过上行流量
    isOver = isMonthUploadOver()
    if isOver:
        deny_limit = 1
        denyUpload(up_rate_limit)
        logger.warning("当月超过上行流量,禁止上传")

    # 判断是否存在记录
    logs = c.execute("select * from log where day_time='%s'  limit 1" % (today))
    row = c.fetchone()
    if row == None:
        if isOver is False:
            # 设置指定上行速率
            setUploadLimit()
        c.execute(
            "INSERT INTO \"log\" (\"day_time\", \"total_upload_first\", \"total_download_first\", \"upload\", \"down\", \"deny_limit\", \"created_at\", \"updated_at\") VALUES ( '%s', %d, %d, %d, %d, %d, '%s', '%s')" % (
                today, total_upload, total_download, upload, down, deny_limit, now, now))
        conn.commit()
        logger.info("初始化今天数据")

    else:
        upload = max(total_upload - row[2], 0)
        down = max(total_download - row[3], 0)
        # 超过设置上传阀值则限制上传
        if upload > uploadLimitEveryDay:
            deny_limit = 1
            logger.warning("当天超过上行流量，限制上传")
            # if row[6] == 0:
            denyUpload(up_rate_limit)

        if total_upload >= row[2]:
            c.execute(
                "UPDATE log  set upload=%d,down=%d,deny_limit=%d,updated_at='%s' where day_time='%s' " % (upload,
                                                                                                          down,
                                                                                                          deny_limit,
                                                                                                          now, today))
            conn.commit()
            logger.info("更新今天数据：upload:%d,down:%d,deny_limit:%d,updated_at=%s" % (upload, down, deny_limit, now))
        else:
            # 如果总上行变小（qbit数据被重置过），则更新total_upload_first和total_download_first
            c.execute(
                "UPDATE log  set total_upload_first=%d,total_download_first=%d,upload=%d,down=%d,deny_limit=%d,updated_at='%s' where day_time='%s' " % (total_upload,total_download,upload,
                                                                                                          down,
                                                                                                          deny_limit,
                                                                                                          now, today))
            conn.commit()
            logger.info("更新今天数据：total_upload_first:%d,total_download_first:%d,upload:%d,down:%d,deny_limit:%d,updated_at=%s" % (total_upload,total_download,upload, down, deny_limit, now))



        logger.info("今天上行流量：%s" % (humanize.naturalsize(upload, binary=True)))
        logger.info("今天下行流量：%s" % (humanize.naturalsize(down, binary=True)))

        if deny_limit==1:
            denyUpload(up_rate_limit)
            logger.warning("执行禁止限速")
        else:
            if uploadSpeedLimit != up_rate_limit:
                setUploadLimit()
                logger.log("执行限速")

    pass


# 判断当月上传流量是否超出限制
def isMonthUploadOver():
    monthFirstDay = datetime.now().strftime("%Y-%m-01")
    today = datetime.now().strftime("%Y-%m-%d")
    c.execute("select sum(upload),sum(down) from log where day_time BETWEEN '%s' and '%s'  limit 1" % (monthFirstDay, today))
    row = c.fetchone()
    if row[0] == None:
        return False
    else:
        logger.info("当月总上行流量：%s" % (humanize.naturalsize(row[0], binary=True)))
        logger.info("当月总下行流量：%s" % (humanize.naturalsize(row[1], binary=True)))
        if row[0] > uploadLimitEveryMonth:
            return True

    return False


# Flask路由
@app.route('/')
def index():
    c2=conn.cursor()
    # 获取最近30天的日志记录
    logs = c2.execute("select id,day_time,upload,down,deny_limit,created_at,updated_at from log order by id desc limit 30")
    rows = c2.fetchall()
    
    # 格式化数据
    formatted_logs = []
    for row in rows:
        formatted_logs.append([
            row[1],  # day_time
            humanize.naturalsize(row[2], binary=True),  # upload
            humanize.naturalsize(row[3], binary=True),  # down
            "是" if row[4] else "否",  # deny_limit
            row[5],  # created_at
            row[6]   # updated_at
        ])
    
    # 获取当月总流量
    monthFirstDay = datetime.now().strftime("%Y-%m-01")
    today = datetime.now().strftime("%Y-%m-%d")
    c2.execute(
        "select sum(upload),sum(down) from log where day_time BETWEEN '%s' and '%s' limit 1" % (monthFirstDay, today))
    month_row = c2.fetchone()
    
    month_upload = "0 B"
    month_download = "0 B"
    if month_row[0] is not None:
        month_upload = humanize.naturalsize(month_row[0], binary=True)
        month_download = humanize.naturalsize(month_row[1], binary=True)
    
    return render_template_string(HTML_TEMPLATE, 
                                logs=formatted_logs, 
                                month_upload=month_upload, 
                                month_download=month_download)


# 启动Flask web服务的函数
def start_web_service():
    app.run(host='0.0.0.0', port=8091, debug=False, use_reloader=False)

if __name__ == '__main__':
    # 设置限速
    setUploadLimit()
    # updateLog()
    schedule.every(10).seconds.do(updateLog)
    
    # 启动Web服务线程
    web_thread = threading.Thread(target=start_web_service, daemon=True)
    web_thread.start()
    
    logger.info("程序执行中")
    logger.info("Web服务已启动在 http://0.0.0.0:8091")
    
    while True:
        schedule.run_pending()  # 检查并执行待执行的任务
        time.sleep(1)  # 休眠1秒避免CPU占用过高