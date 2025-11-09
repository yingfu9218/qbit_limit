// API endpoint
const API_BASE = window.location.origin;
const REFRESH_DATA_URL = `${API_BASE}/refresh_data`;

// Fetch data from API
async function fetchData() {
    try {
        const response = await fetch(REFRESH_DATA_URL);
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        const data = await response.json();
        return data;
    } catch (error) {
        console.error('Error fetching data:', error);
        throw error;
    }
}

// Update month statistics
function updateMonthStats(data) {
    const monthUploadEl = document.getElementById('month-upload');
    const monthDownloadEl = document.getElementById('month-download');
    
    console.log('Updating month stats:', data.month_upload, data.month_download); // Debug log
    
    if (monthUploadEl) {
        monthUploadEl.textContent = data.month_upload || '0 B';
        console.log('Updated month-upload element:', monthUploadEl.textContent);
    } else {
        console.error('month-upload element not found');
    }
    
    if (monthDownloadEl) {
        monthDownloadEl.textContent = data.month_download || '0 B';
        console.log('Updated month-download element:', monthDownloadEl.textContent);
    } else {
        console.error('month-download element not found');
    }
}

// Create log card HTML (Mobile)
function createLogCardMobile(log) {
    return `
        <div class="log-card">
            <div class="log-card-mobile">
                <div class="log-date-header">
                    <svg class="log-date-icon" xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                        <rect x="3" y="4" width="18" height="18" rx="2" ry="2"></rect>
                        <line x1="16" y1="2" x2="16" y2="6"></line>
                        <line x1="8" y1="2" x2="8" y2="6"></line>
                        <line x1="3" y1="10" x2="21" y2="10"></line>
                    </svg>
                    <span>${log.date}</span>
                </div>
                
                <div class="log-separator"></div>
                
                <div class="log-traffic-grid">
                    <div class="log-traffic-item">
                        <div class="log-traffic-label">
                            <svg class="log-traffic-icon" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <circle cx="12" cy="12" r="10"></circle>
                                <polyline points="16 12 12 8 8 12"></polyline>
                                <line x1="12" y1="16" x2="12" y2="8"></line>
                            </svg>
                            <span>上传</span>
                        </div>
                        <div class="log-traffic-value">${log.upload}</div>
                    </div>
                    
                    <div class="log-traffic-item">
                        <div class="log-traffic-label">
                            <svg class="log-traffic-icon" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <circle cx="12" cy="12" r="10"></circle>
                                <polyline points="8 12 12 16 16 12"></polyline>
                                <line x1="12" y1="8" x2="12" y2="16"></line>
                            </svg>
                            <span>下载</span>
                        </div>
                        <div class="log-traffic-value">${log.download}</div>
                    </div>
                </div>
                
                <div class="log-separator"></div>
                
                <div class="log-status">
                    <div class="log-status-label">
                        <svg class="log-status-icon" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <circle cx="12" cy="12" r="10"></circle>
                            <line x1="12" y1="8" x2="12" y2="12"></line>
                            <line x1="12" y1="16" x2="12.01" y2="16"></line>
                        </svg>
                        <span>是否触发限速</span>
                    </div>
                    <span class="badge ${log.limited === '是' ? 'badge-danger' : 'badge-success'}">${log.limited}</span>
                </div>
            </div>
            
            <div class="log-card-desktop">
                <div class="log-grid">
                    <div class="log-item">
                        <div class="log-icon-wrapper date">
                            <svg class="log-icon date" xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <rect x="3" y="4" width="18" height="18" rx="2" ry="2"></rect>
                                <line x1="16" y1="2" x2="16" y2="6"></line>
                                <line x1="8" y1="2" x2="8" y2="6"></line>
                                <line x1="3" y1="10" x2="21" y2="10"></line>
                            </svg>
                        </div>
                        <div class="log-content">
                            <div class="log-label">日期</div>
                            <div class="log-value">${log.date}</div>
                        </div>
                    </div>
                    
                    <div class="log-item">
                        <div class="log-icon-wrapper upload">
                            <svg class="log-icon upload" xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <circle cx="12" cy="12" r="10"></circle>
                                <polyline points="16 12 12 8 8 12"></polyline>
                                <line x1="12" y1="16" x2="12" y2="8"></line>
                            </svg>
                        </div>
                        <div class="log-content">
                            <div class="log-label">上传</div>
                            <div class="log-value">${log.upload}</div>
                        </div>
                    </div>
                    
                    <div class="log-item">
                        <div class="log-icon-wrapper download">
                            <svg class="log-icon download" xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <circle cx="12" cy="12" r="10"></circle>
                                <polyline points="8 12 12 16 16 12"></polyline>
                                <line x1="12" y1="8" x2="12" y2="16"></line>
                            </svg>
                        </div>
                        <div class="log-content">
                            <div class="log-label">下载</div>
                            <div class="log-value">${log.download}</div>
                        </div>
                    </div>
                    
                    <div class="log-item">
                        <div class="log-icon-wrapper status">
                            <svg class="log-icon status" xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                                <circle cx="12" cy="12" r="10"></circle>
                                <line x1="12" y1="8" x2="12" y2="12"></line>
                                <line x1="12" y1="16" x2="12.01" y2="16"></line>
                            </svg>
                        </div>
                        <div class="log-content">
                            <div class="log-label">是否触发限速</div>
                            <span class="badge ${log.limited === '是' ? 'badge-danger' : 'badge-success'}">${log.limited}</span>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `;
}

// Update logs list
function updateLogsList(logs) {
    const logsListEl = document.getElementById('logs-list');
    if (!logsListEl) {
        console.error('logs-list element not found');
        return;
    }
    
    console.log('Updating logs list, count:', logs ? logs.length : 0);
    
    if (!logs || logs.length === 0) {
        logsListEl.innerHTML = '<div class="loading">暂无数据</div>';
        return;
    }
    
    const html = logs.map(log => {
        console.log('Processing log:', log);
        return createLogCardMobile(log);
    }).join('');
    
    logsListEl.innerHTML = html;
    console.log('Logs list updated, HTML length:', html.length);
}

// Initialize and load data
async function init() {
    const logsListEl = document.getElementById('logs-list');
    if (logsListEl) {
        logsListEl.innerHTML = '<div class="loading">加载中...</div>';
    }
    
    try {
        const data = await fetchData();
        console.log('Fetched data:', data); // Debug log
        
        if (data) {
            updateMonthStats(data);
            if (data.logs && Array.isArray(data.logs)) {
                updateLogsList(data.logs);
            } else {
                console.error('Invalid logs data:', data.logs);
                if (logsListEl) {
                    logsListEl.innerHTML = '<div class="error">数据格式错误</div>';
                }
            }
        } else {
            console.error('No data received');
            if (logsListEl) {
                logsListEl.innerHTML = '<div class="error">未获取到数据</div>';
            }
        }
    } catch (error) {
        console.error('Error in init:', error);
        const logsListEl = document.getElementById('logs-list');
        if (logsListEl) {
            logsListEl.innerHTML = '<div class="error">加载数据失败，请稍后重试: ' + error.message + '</div>';
        }
    }
}

// Auto refresh every 60 seconds
setInterval(init, 60000);

// Initial load
init();

