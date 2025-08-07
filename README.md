## qbit_limit
限制qbittorrent每天、每月上行流量，以及上行速率，防止限速过小影响pt上传流量，又防止没限速上传流量过大被运营商检测到，目前有些运营商会监测大上行的流量宽带用户（当使用pcdn处理），监测到大流量上传可能会限速或断网，这个工具可以控制每天和每月的上传流量上限。

功能：
1. 设置qbittorrent上行限速
2. 设置qbittorrent每天上行总流量，超过设置流量禁用上传，第二天自动恢复
3. 设置qbittorrent每月上行总流量，超过设置流量禁用上传，下个月自动恢复

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


查看每天日志
```
python3 logs.py

```