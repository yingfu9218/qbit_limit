import os
import sqlite3
from loguru import logger
import humanize
from prettytable import PrettyTable
from datetime import datetime


dbPath = os.path.dirname(os.path.abspath(__file__))
conn = sqlite3.connect(dbPath + '/qb.db')
c = conn.cursor()

if __name__ == '__main__':
    # 判断是否存在记录
    logs = c.execute("select id,day_time,upload,down,deny_limit,created_at,updated_at from log  order by id desc  limit 30 " )
    rows = c.fetchall()
    table = PrettyTable([ '日期', '上传', '下载','是否触发限速','创建时间','更新时间'])
    for row in rows:
        table.add_row([row[1],humanize.naturalsize(row[2], binary=True), humanize.naturalsize(row[3], binary=True),row[4],row[5],row[6]])

    print(table)

    # 本月总流量

    monthFirstDay = datetime.now().strftime("%Y-%m-01")
    today = datetime.now().strftime("%Y-%m-%d")
    c.execute(
        "select sum(upload),sum(down) from log where day_time BETWEEN '%s' and '%s'  limit 1" % (monthFirstDay, today))
    row = c.fetchone()
    if row[0] != None:
        print("当月总上行流量：%s" % (humanize.naturalsize(row[0], binary=True)))
        print("当月总下行流量：%s" % (humanize.naturalsize(row[1], binary=True)))

