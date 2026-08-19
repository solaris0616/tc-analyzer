let currentBroadcasterID = null;
let currentMovieID = null;
let viewerChart = null;
let analysisChart = null;
let autoRefreshTimer = null;
let autoRefreshInterval = 10; // seconds (0 = off)
let refreshGeneration = 0;

// Design system chart global defaults
Chart.defaults.font.family = "'Inter', system-ui, -apple-system, sans-serif";
Chart.defaults.font.size = 12;
Chart.defaults.color = '#94a3b8';

document.addEventListener('DOMContentLoaded', () => {
    initUI();
    initAutoRefresh();
    refreshAllData(false);
});

function initUI() {
    // Mobile menu toggle logic
    const menuToggle = document.getElementById('menu-toggle');
    const sidebar = document.getElementById('sidebar');
    if (menuToggle && sidebar) {
        menuToggle.addEventListener('click', (e) => {
            e.stopPropagation();
            sidebar.classList.toggle('open');
        });

        document.addEventListener('click', (e) => {
            if (sidebar.classList.contains('open') && !sidebar.contains(e.target) && e.target !== menuToggle) {
                sidebar.classList.remove('open');
            }
        });
    }

    // Manual Refresh button
    const refreshBtn = document.getElementById('refresh-btn');
    if (refreshBtn) {
        refreshBtn.addEventListener('click', () => {
            const refreshIcon = refreshBtn.querySelector('.btn-icon');
            if (refreshIcon) {
                refreshIcon.style.transition = 'transform 0.5s ease';
                refreshIcon.style.transform = 'rotate(360deg)';
                setTimeout(() => { refreshIcon.style.transform = 'none'; }, 500);
            }
            refreshAllData(false);
        });
    }
}

function initAutoRefresh() {
    const select = document.getElementById('auto-refresh-select');
    if (!select) return;

    // Load saved preference
    const saved = localStorage.getItem('tc_auto_refresh');
    if (saved !== null && !isNaN(parseInt(saved, 10))) {
        autoRefreshInterval = parseInt(saved, 10);
        select.value = autoRefreshInterval;
    } else {
        autoRefreshInterval = parseInt(select.value, 10) || 10;
    }

    select.addEventListener('change', () => {
        autoRefreshInterval = parseInt(select.value, 10);
        localStorage.setItem('tc_auto_refresh', autoRefreshInterval);
        setupTimer();
    });

    // Handle background tab switching to save resources
    document.addEventListener('visibilitychange', () => {
        if (document.hidden) {
            clearTimer();
        } else {
            setupTimer();
            if (autoRefreshInterval > 0) {
                refreshAllData(true);
            }
        }
    });

    setupTimer();
}

function setupTimer() {
    clearTimer();
    const indicator = document.getElementById('live-indicator');
    const statusLabel = document.getElementById('live-status-label');

    if (autoRefreshInterval > 0) {
        if (indicator) indicator.classList.add('active');
        if (statusLabel) statusLabel.textContent = `${autoRefreshInterval}s`;

        autoRefreshTimer = setInterval(() => {
            refreshAllData(true);
        }, autoRefreshInterval * 1000);
    } else {
        if (indicator) indicator.classList.remove('active');
        if (statusLabel) statusLabel.textContent = 'OFF';
    }
}

function clearTimer() {
    if (autoRefreshTimer) {
        clearInterval(autoRefreshTimer);
        autoRefreshTimer = null;
    }
}

function updateLastUpdatedDisplay() {
    const el = document.getElementById('last-updated-text');
    if (el) {
        const now = new Date();
        el.textContent = `最終更新 ${now.toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit', second: '2-digit' })}`;
    }
}

async function refreshAllData(isSilent = false) {
    const generation = ++refreshGeneration;
    try {
        await loadBroadcasters(generation);
        if (generation !== refreshGeneration || !currentBroadcasterID) return;

        const movies = await loadMovieList(isSilent, generation);
        if (generation !== refreshGeneration) return;

        if (!movies || movies.length === 0) {
            currentMovieID = null;
            clearMovieDetail();
            await loadAnalysisData(isSilent, generation);
        } else {
            if (!currentMovieID || !movies.some(m => m.movie_id === currentMovieID)) {
                currentMovieID = movies[0].movie_id;
            }
            await Promise.all([
                loadMovieDetail(currentMovieID, isSilent, generation),
                loadAnalysisData(isSilent, generation)
            ]);
        }
        if (generation !== refreshGeneration) return;
        updateLastUpdatedDisplay();
    } catch (err) {
        console.error('Error during data refresh:', err);
    }
}

async function loadBroadcasters(generation) {
    const res = await fetch('/api/broadcasters');
    if (!res.ok) throw new Error('Failed to fetch broadcasters');
    const broadcasters = await res.json();
    if (generation !== refreshGeneration) return;

    const select = document.getElementById('broadcaster-select');
    if (!select) return;

    const savedID = localStorage.getItem('tc_broadcaster_id');
    const availableIDs = new Set(broadcasters.map(b => b.id));
    if (!currentBroadcasterID || !availableIDs.has(currentBroadcasterID)) {
        currentBroadcasterID = availableIDs.has(savedID) ? savedID : (broadcasters[0]?.id || null);
        currentMovieID = null;
    }

    select.innerHTML = '';
    if (broadcasters.length === 0) {
        select.innerHTML = '<option value="">配信者データがありません</option>';
        select.disabled = true;
        clearMovieDetail();
        const container = document.getElementById('movie-list');
        if (container) container.innerHTML = '<div class="loading-spinner-container"><span>新規収集またはバックフィルを実行してください</span></div>';
        return;
    }

    broadcasters.forEach(b => {
        const option = document.createElement('option');
        option.value = b.id;
        const displayName = b.name || b.screen_id || b.id;
        const screenID = b.screen_id ? ` (@${b.screen_id})` : '';
        option.textContent = `${displayName}${screenID} — ${b.movie_count}配信`;
        select.appendChild(option);
    });
    select.value = currentBroadcasterID;
    select.disabled = false;

    if (!select.hasAttribute('data-bound')) {
        select.setAttribute('data-bound', 'true');
        select.addEventListener('change', () => {
            currentBroadcasterID = select.value || null;
            currentMovieID = null;
            _allCommenters = [];
            if (currentBroadcasterID) localStorage.setItem('tc_broadcaster_id', currentBroadcasterID);
            refreshAllData(false);
        });
    }
}

function clearMovieDetail() {
    ['stat-max-viewers', 'stat-avg-viewers', 'stat-comments', 'stat-records', 'stat-commenter-count']
        .forEach(id => { const el = document.getElementById(id); if (el) el.textContent = '-'; });
    const title = document.getElementById('selected-title');
    const subtitle = document.getElementById('selected-subtitle');
    if (title) title.textContent = 'ダッシュボード';
    if (subtitle) {
        subtitle.textContent = currentBroadcasterID ? '配信データがありません' : '配信者を選択してください';
        subtitle.className = 'status-badge badge-neutral';
    }
    renderViewerChart([], false);
    renderAnalysisChart([]);
    const tbody = document.getElementById('commenters-tbody');
    if (tbody) tbody.innerHTML = '<tr><td colspan="6" class="empty-row">配信を選択してください</td></tr>';
}

function formatShortDateTime(isoStr) {
    if (!isoStr) return '-';
    const d = new Date(isoStr);
    if (isNaN(d.getTime())) return '-';
    return d.toLocaleDateString('ja-JP', { month: '2-digit', day: '2-digit' }) + ' ' +
           d.toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' });
}

function formatFullDateTime(isoStr) {
    if (!isoStr) return '-';
    const d = new Date(isoStr);
    if (isNaN(d.getTime())) return '-';
    return d.getFullYear() + '/' +
           String(d.getMonth() + 1).padStart(2, '0') + '/' +
           String(d.getDate()).padStart(2, '0') + ' ' +
           String(d.getHours()).padStart(2, '0') + ':' +
           String(d.getMinutes()).padStart(2, '0') + ':' +
           String(d.getSeconds()).padStart(2, '0');
}

async function loadMovieList(isSilent = false, generation = refreshGeneration) {
    try {
        if (!currentBroadcasterID) return [];
        const res = await fetch(`/api/movies?broadcaster_id=${encodeURIComponent(currentBroadcasterID)}`);
        if (!res.ok) throw new Error('Failed to fetch movies');
        const movies = await res.json();
        if (generation !== refreshGeneration) return [];

        if (movies.length > 0 && (!currentMovieID || !movies.some(m => m.movie_id === currentMovieID))) {
            currentMovieID = movies[0].movie_id;
        }

        const container = document.getElementById('movie-list');
        const countBadge = document.getElementById('session-count');

        if (countBadge) {
            countBadge.textContent = movies ? movies.length : 0;
        }

        if (!movies || movies.length === 0) {
            container.innerHTML = '<div class="loading-spinner-container"><span>対象のデータがありません</span></div>';
            return [];
        }

        // Preserve scroll position if re-rendering silently
        const prevScrollTop = container.scrollTop;
        container.innerHTML = '';

        movies.forEach((m) => {
            const isActive = m.movie_id === currentMovieID;
            const item = document.createElement('div');
            item.className = 'movie-item' + (isActive ? ' active' : '');

            const displayTitle = m.title ? m.title : `Movie #${m.movie_id}`;
            const dateStr = formatShortDateTime(m.started_at);
            const labelHtml = m.label ? `<span class="movie-item-tag" title="${escapeHtml(m.label)}">${escapeHtml(m.label)}</span>` : '';

            item.innerHTML = `
                <div class="movie-item-header">
                    <span class="movie-item-title" title="${escapeHtml(displayTitle)}">${escapeHtml(displayTitle)}</span>
                    <span class="movie-item-snapshots">${m.total_records} pts</span>
                </div>
                <div class="movie-item-meta">
                    <span class="movie-item-date">${dateStr}</span>
                    ${labelHtml}
                </div>
            `;
            item.addEventListener('click', () => {
                document.querySelectorAll('.movie-item').forEach(el => el.classList.remove('active'));
                item.classList.add('active');

                // Close sidebar on mobile select
                const sidebar = document.getElementById('sidebar');
                if (sidebar && sidebar.classList.contains('open')) {
                    sidebar.classList.remove('open');
                }

                currentMovieID = m.movie_id;
                loadMovieDetail(m.movie_id, false);
            });
            container.appendChild(item);
        });

        if (isSilent) {
            container.scrollTop = prevScrollTop;
        }
        return movies;
    } catch (err) {
        console.error('Error loading movie list:', err);
        return [];
    }
}

async function loadMovieDetail(movieID, isSilent = false, generation = refreshGeneration) {
    currentMovieID = movieID;
    try {
        const res = await fetch(`/api/movies/${movieID}`);
        if (!res.ok) throw new Error('Failed to fetch movie detail');
        const data = await res.json();
        if (generation !== refreshGeneration || movieID !== currentMovieID) return;

        const titleEl = document.getElementById('selected-title');
        const subtitleEl = document.getElementById('selected-subtitle');
        const metaIdEl = document.querySelector('#meta-movie-id span');
        const metaDateEl = document.querySelector('#meta-date span');
        const metaIntervalEl = document.querySelector('#meta-interval span');

        const movieTitle = data.movie && data.movie.title ? data.movie.title : `Movie #${movieID}`;
        if (titleEl) {
            titleEl.textContent = movieTitle;
            titleEl.title = movieTitle;
        }

        if (subtitleEl) {
            const labelText = data.movie && data.movie.label ? data.movie.label : (data.movie && data.movie.title ? `ID: ${movieID}` : '配信データ');
            subtitleEl.textContent = labelText;
            subtitleEl.className = 'status-badge badge-active';
        }

        if (metaIdEl) {
            metaIdEl.textContent = `ID: ${movieID}`;
        }

        if (metaDateEl) {
            const startedAt = data.movie ? data.movie.started_at : (data.summary ? data.summary.first_record : null);
            metaDateEl.textContent = `開始: ${formatFullDateTime(startedAt)}`;
        }

        if (metaIntervalEl) {
            const interval = data.movie && data.movie.interval_sec ? data.movie.interval_sec : 10;
            metaIntervalEl.textContent = `間隔: ${interval}秒`;
        }

        if (data.summary) {
            document.getElementById('stat-max-viewers').textContent = data.summary.peak_viewers.toLocaleString();
            document.getElementById('stat-avg-viewers').textContent = Math.round(data.summary.avg_viewers).toLocaleString();
            const commentTotal = data.summary.final_comment_count ?? data.summary.total_comments_observed ?? 0;
            document.getElementById('stat-comments').textContent = commentTotal.toLocaleString();
            document.getElementById('stat-records').textContent = data.summary.total_records.toLocaleString();
        }

        const commenterCountEl = document.getElementById('stat-commenter-count');
        if (commenterCountEl) {
            commenterCountEl.textContent = (data.commenter_count ?? 0).toLocaleString();
        }

        renderViewerChart(data.snapshots || [], isSilent);
        loadCommenters(movieID, isSilent);
    } catch (err) {
        console.error('Error loading movie detail:', err);
    }
}

function renderViewerChart(snapshots, isSilent = false) {
    const canvas = document.getElementById('viewerChart');
    if (!canvas) return;
    const ctx = canvas.getContext('2d');

    const labels = snapshots.map(s => new Date(s.recorded_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }));
    const viewers = snapshots.map(s => s.current_view_count);
    const comments = snapshots.map(s => s.comment_count);
    const commenters = snapshots.map(s => s.cumulative_commenters ?? 0);

    // If chart already exists and we just need an update
    if (viewerChart) {
        viewerChart.data.labels = labels;
        viewerChart.data.datasets[0].data = viewers;
        viewerChart.data.datasets[1].data = comments;
        viewerChart.data.datasets[2].data = commenters;
        viewerChart.data.datasets[0].pointRadius = snapshots.length > 50 ? 0 : 3;
        viewerChart.data.datasets[2].pointRadius = snapshots.length > 50 ? 0 : 2;
        viewerChart.update(isSilent ? 'none' : 'default');
        return;
    }

    // Create custom smooth gradient for area fill
    const blueGradient = ctx.createLinearGradient(0, 0, 0, 300);
    blueGradient.addColorStop(0, 'rgba(59, 130, 246, 0.35)');
    blueGradient.addColorStop(1, 'rgba(59, 130, 246, 0.0)');

    viewerChart = new Chart(ctx, {
        type: 'line',
        data: {
            labels: labels,
            datasets: [
                {
                    label: '同時視聴者数',
                    data: viewers,
                    borderColor: '#3b82f6',
                    borderWidth: 2.5,
                    backgroundColor: blueGradient,
                    fill: true,
                    tension: 0.35,
                    pointRadius: snapshots.length > 50 ? 0 : 3,
                    pointHoverRadius: 6,
                    pointBackgroundColor: '#3b82f6',
                    yAxisID: 'y'
                },
                {
                    label: '累計コメント数',
                    data: comments,
                    borderColor: '#8b5cf6',
                    borderWidth: 2,
                    backgroundColor: 'transparent',
                    borderDash: [4, 4],
                    tension: 0.35,
                    pointRadius: 0,
                    pointHoverRadius: 5,
                    pointBackgroundColor: '#8b5cf6',
                    yAxisID: 'y1'
                },
                {
                    label: '累計コメンター数',
                    data: commenters,
                    borderColor: '#10b981',
                    borderWidth: 2,
                    backgroundColor: 'transparent',
                    tension: 0.3,
                    pointRadius: snapshots.length > 50 ? 0 : 2,
                    pointHoverRadius: 5,
                    pointBackgroundColor: '#10b981',
                    yAxisID: 'y'
                }
            ]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: {
                mode: 'index',
                intersect: false
            },
            plugins: {
                legend: {
                    display: false
                },
                tooltip: {
                    backgroundColor: 'rgba(15, 23, 42, 0.9)',
                    titleColor: '#f8fafc',
                    bodyColor: '#cbd5e1',
                    borderColor: 'rgba(255, 255, 255, 0.1)',
                    borderWidth: 1,
                    padding: 12,
                    cornerRadius: 8,
                    displayColors: true,
                    usePointStyle: true
                }
            },
            scales: {
                x: {
                    grid: { color: 'rgba(255, 255, 255, 0.04)' },
                    ticks: { color: '#64748b', maxRotation: 0 }
                },
                y: {
                    type: 'linear',
                    display: true,
                    position: 'left',
                    grid: { color: 'rgba(255, 255, 255, 0.04)' },
                    ticks: { color: '#94a3b8' },
                    title: { display: true, text: '視聴者・コメンター数 (人)', color: '#64748b', font: { size: 11 } }
                },
                y1: {
                    type: 'linear',
                    display: true,
                    position: 'right',
                    grid: { drawOnChartArea: false },
                    ticks: { color: '#94a3b8' },
                    title: { display: true, text: 'コメント数 (件)', color: '#64748b', font: { size: 11 } }
                }
            }
        }
    });
}

async function loadAnalysisData(isSilent = false, generation = refreshGeneration) {
    try {
        if (!currentBroadcasterID) return;
        const res = await fetch(`/api/analysis?broadcaster_id=${encodeURIComponent(currentBroadcasterID)}`);
        if (!res.ok) throw new Error('Failed to fetch analysis data');
        const rows = await res.json();
        if (generation !== refreshGeneration) return;

        renderAnalysisChart(rows || [], isSilent);
    } catch (err) {
        console.error('Error loading analysis data:', err);
    }
}

function renderAnalysisChart(rows, isSilent = false) {
    const canvas = document.getElementById('analysisChart');
    if (!canvas) return;
    const ctx = canvas.getContext('2d');

    const days = ['日曜日', '月曜日', '火曜日', '水曜日', '木曜日', '金曜日', '土曜日'];
    const dayAvgs = new Array(7).fill(0);
    const dayCounts = new Array(7).fill(0);

    rows.forEach(r => {
        if (r.day_of_week >= 0 && r.day_of_week < 7) {
            dayAvgs[r.day_of_week] += r.avg_viewers * r.data_points;
            dayCounts[r.day_of_week] += r.data_points;
        }
    });

    const finalAvgs = dayAvgs.map((sum, idx) => dayCounts[idx] > 0 ? Math.round(sum / dayCounts[idx]) : 0);

    if (analysisChart) {
        analysisChart.data.datasets[0].data = finalAvgs;
        analysisChart.update(isSilent ? 'none' : 'default');
        return;
    }

    analysisChart = new Chart(ctx, {
        type: 'bar',
        data: {
            labels: days,
            datasets: [{
                label: '平均同時視聴者数',
                data: finalAvgs,
                backgroundColor: [
                    'rgba(244, 63, 94, 0.7)',
                    'rgba(59, 130, 246, 0.7)',
                    'rgba(16, 185, 129, 0.7)',
                    'rgba(245, 158, 11, 0.7)',
                    'rgba(139, 92, 246, 0.7)',
                    'rgba(236, 72, 153, 0.7)',
                    'rgba(99, 102, 241, 0.7)'
                ],
                borderColor: [
                    '#f43f5e',
                    '#3b82f6',
                    '#10b981',
                    '#f59e0b',
                    '#8b5cf6',
                    '#ec4899',
                    '#6366f1'
                ],
                borderWidth: 1,
                borderRadius: 8,
                borderSkipped: false
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: { display: false },
                tooltip: {
                    backgroundColor: 'rgba(15, 23, 42, 0.9)',
                    titleColor: '#f8fafc',
                    bodyColor: '#cbd5e1',
                    borderColor: 'rgba(255, 255, 255, 0.1)',
                    borderWidth: 1,
                    padding: 12,
                    cornerRadius: 8
                }
            },
            scales: {
                x: {
                    grid: { display: false },
                    ticks: { color: '#94a3b8' }
                },
                y: {
                    grid: { color: 'rgba(255, 255, 255, 0.04)' },
                    ticks: { color: '#64748b' },
                    title: { display: true, text: '平均視聴者数 (人)', color: '#64748b', font: { size: 11 } }
                }
            }
        }
    });
}

let _allCommenters = [];

async function loadCommenters(movieID, isSilent = false) {
    const tbody = document.getElementById('commenters-tbody');
    if (!tbody) return;

    if (!isSilent && _allCommenters.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" class="empty-row">読み込み中...</td></tr>';
    }

    try {
        const res = await fetch(`/api/movies/${movieID}/commenters`);
        if (!res.ok) throw new Error('Failed to fetch commenters');
        _allCommenters = await res.json();

        const searchEl = document.getElementById('commenter-search');
        const currentFilter = searchEl ? searchEl.value.trim().toLowerCase() : '';

        if (searchEl && !searchEl.hasAttribute('data-bound')) {
            searchEl.setAttribute('data-bound', 'true');
            searchEl.oninput = () => {
                renderCommentersTable(_allCommenters, searchEl.value.trim().toLowerCase());
            };
        }

        renderCommentersTable(_allCommenters, currentFilter);
    } catch (err) {
        console.error('Error loading commenters:', err);
        if (tbody && !isSilent) {
            tbody.innerHTML = '<tr><td colspan="6" class="empty-row">データ取得エラー</td></tr>';
        }
    }
}

function renderCommentersTable(commenters, filter) {
    const tbody = document.getElementById('commenters-tbody');
    if (!tbody) return;

    const filtered = filter
        ? commenters.filter(c =>
            (c.name && c.name.toLowerCase().includes(filter)) ||
            (c.screen_id && c.screen_id.toLowerCase().includes(filter)) ||
            (c.user_id && c.user_id.toLowerCase().includes(filter)))
        : commenters;

    if (!filtered || filtered.length === 0) {
        tbody.innerHTML = `<tr><td colspan="6" class="empty-row">${filter ? '該当するユーザーが見つかりません' : 'コメントデータがありません'}</td></tr>`;
        return;
    }

    const fmtDate = iso => {
        if (!iso) return '-';
        const d = new Date(iso);
        return d.toLocaleDateString('ja-JP', { month: '2-digit', day: '2-digit' })
             + ' ' + d.toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' });
    };

    tbody.innerHTML = filtered.map((c, i) => `
        <tr>
            <td class="col-rank">${i + 1}</td>
            <td class="col-name"><span class="commenter-name">${escapeHtml(c.name)}</span></td>
            <td class="col-screenid"><span class="commenter-screenid">@${escapeHtml(c.screen_id)}</span></td>
            <td class="col-num"><span class="comment-count-badge">${c.comment_count.toLocaleString()}</span></td>
            <td class="col-date">${fmtDate(c.first_seen_at)}</td>
            <td class="col-date">${fmtDate(c.last_seen_at)}</td>
        </tr>
    `).join('');
}

function escapeHtml(str) {
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}
