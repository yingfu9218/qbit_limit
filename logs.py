import os
import sqlite3
from loguru import logger
import humanize
from prettytable import PrettyTable


dbPath = os.path.dirname(os.path.abspath(__file__))
conn = sqlite3.connect(dbPath + '/qb.db')
c = conn.cursor()

if __name__ == '__main__':
    # 判断是否存在记录
    logs = c.execute("select id,day_time,upload,down,deny_limit,created_at,updated_at from log  order by id desc  limit 30 " )
    rows = c.fetchall()
    table = PrettyTable(['id', 'day_time', 'upload', 'down','deny_limit','created_at','updated_at'])
    for row in rows:
        table.add_row([row[0],row[1],humanize.naturalsize(row[2], binary=True), humanize.naturalsize(row[3], binary=True),row[4],row[5],row[6]])

    print(table)
