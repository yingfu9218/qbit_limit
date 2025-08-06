## qbit_limit
限制qbittorrent每天、每月上行流量，以及上行速率。防止上传过大被运营商检测到。

#### 安装依赖
```
pip3 install -r requirements.txt
```

#### 修改配置
```
cp config.example.py config.py

vim config.py 修改qbittorrent账号信息和上行限制配置

```
执行 (放在后台一直执行，也可以用supervisorctl 守护执行)
```
python3 qbit_limit.py

```