import os
import sqlite3
from flask import Flask, render_template_string
import humanize
from datetime import datetime

app = Flask(__name__)

# 数据库连接
dbPath = os.path.dirname(os.path.abspath(__file__))
conn = sqlite3.connect(dbPath + '/qb.db', check_same_thread=False)
c = conn.cursor()

HTML_TEMPLATE = """
<!DOCTYPE html>
<html>
<head>
    <title>流量统计查看</title>
    <meta charset="UTF-8">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        table { border-collapse: collapse; width: 100%; }
        th, td { border: 1px solid #ddd; padding: 12px; text-align: left; }
        th { background-color: #f2f2f2; }
        tr:nth-child(even) { background-color: #f9f9f9; }
        .summary { margin: 20px 0; padding: 15px; background-color: #e7f3ff; border-radius: 5px; }
        .summary h3 { margin-top: 0; }
        h1 { color: #333; }
    </style>
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
                <td>{{ row[0] }}</td>
                <td>{{ row[1] }}</td>
                <td>{{ row[2] }}</td>
                <td>{{ row[3] }}</td>
                <td>{{ row[4] }}</td>
                <td>{{ row[5] }}</td>
            </tr>
            {% endfor %}
        </tbody>
    </table>
</body>
</html>
"""

@app.route('/')
def index():
    # 获取最近30天的日志记录
    logs = c.execute("select id,day_time,upload,down,deny_limit,created_at,updated_at from log order by id desc limit 30")
    rows = c.fetchall()
    
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
    c.execute(
        "select sum(upload),sum(down) from log where day_time BETWEEN '%s' and '%s' limit 1" % (monthFirstDay, today))
    month_row = c.fetchone()
    
    month_upload = "0 B"
    month_download = "0 B"
    if month_row[0] is not None:
        month_upload = humanize.naturalsize(month_row[0], binary=True)
        month_download = humanize.naturalsize(month_row[1], binary=True)
    
    return render_template_string(HTML_TEMPLATE, 
                                logs=formatted_logs, 
                                month_upload=month_upload, 
                                month_download=month_download)

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=91, debug=True)