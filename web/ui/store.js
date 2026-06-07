document.addEventListener('alpine:init', () => {
    Alpine.store('global', {
        lang: window.getSystemLanguage(),
        theme: window.getSystemTheme(),
        currentTab: 'usage_billing',
        proMode: localStorage.getItem('polarisagi_pro_mode') === 'true',
        toast: { show: false, message: '', type: 'success' },
        apiData: [],
        concurrency: { active: 0, waiting: 0, max: 0 },
        nodes: [],
        routes: [],
        allModels: [],
        settings: {
            listen_addr: '127.0.0.1:27777',
            breaker: {
                initial_cooldown_seconds: 60,
                max_cooldown_seconds: 3600,
                failure_threshold: 3,
                failure_window_seconds: 120
            },
            google_oauth_client_id: '',
            google_oauth_client_secret: '',
            update_proxy_channel: ''
        },
        version: '',
        latestVersion: '',
        updateAvailable: false,
        isUpdating: false,
        debugEnabled: false,
        logLevelFilter: 'all',
        logsText: 'Loading logs...',
        isAutoScroll: true,
        availableAccounts: [],
        selectedAccount: 'all',
        activePreset: 'today',
        startDate: '',
        endDate: '',

        t(key) {
            return window.messages[this.lang][key] || key;
        },

        showToast(msg, type = 'success') {
            this.toast = { show: true, message: msg, type };
            setTimeout(() => { this.toast.show = false }, 3000);
        },

        formatNum(num) { return Number(num || 0).toFixed(4); },
        formatToken(num) { return new Intl.NumberFormat().format(num); },
        formatShortDate(dt) { return dt ? dt.split('T')[0].split(' ')[0] : '-'; },
        
        successRateColor(rate) { 
            return rate > 95 ? 'border-success' : (rate > 80 ? 'border-warning' : 'border-error');
        },

        protocolLabel(p) {
            const labels = { 
                openai: 'OpenAI', google: 'Google (Gemini)', anthropic: 'Anthropic',
                deepseek: 'DeepSeek', siliconflow: 'SiliconFlow', grok: 'Grok',
                openrouter: 'OpenRouter', ollama: 'Ollama'
            };
            return labels[p] || p;
        },
        protocolClass(p) {
            const classes = { openai: 'text-primary', google: 'text-success', anthropic: 'text-warning', deepseek: 'text-info', siliconflow: 'text-secondary', grok: 'text-neutral', openrouter: 'text-accent', ollama: 'text-base-content' };
            return classes[p] || 'text-base-content/50';
        },
        protocolBadge(p) {
            const badges = { openai: 'badge-primary', google: 'badge-success', anthropic: 'badge-warning', deepseek: 'badge-info', siliconflow: 'badge-secondary', grok: 'badge-neutral', openrouter: 'badge-accent', ollama: 'badge-ghost' };
            return badges[p] || 'badge-neutral';
        },

        setLang(l) {
            window.setSystemLanguage(l);
            this.lang = l;
        },

        toggleProMode() {
            this.proMode = !this.proMode;
            localStorage.setItem('polarisagi_pro_mode', this.proMode);
            if (!this.proMode && this.currentTab === 'smart_routing') {
                this.currentTab = 'ai_accounts';
            }
        },

        setTheme(t) {
            this.theme = t;
            window.applyTheme(t);
        },

        checkForUpdates(currentVer) {
            fetch('https://api.github.com/repos/polarisagi/hermes/releases/latest')
                .then(r => r.json())
                .then(d => {
                    if (d.tag_name && d.tag_name !== currentVer) {
                        this.latestVersion = d.tag_name;
                        this.updateAvailable = true;
                    }
                }).catch(() => {});
        },

        triggerUpdate() {
            if (!confirm(this.lang === 'zh' ? `确定要平滑热更新到 ${this.latestVersion} 吗？\n整个过程完全自动化，并且不会中断正在处理的流量。` : `Are you sure you want to smooth update to ${this.latestVersion}?\nThe process is fully automated and will not interrupt active traffic.`)) return;
            
            this.isUpdating = true;
            fetch('/api/admin/update', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ target_version: this.latestVersion })
            }).then(r => r.json()).then(d => {
                this.showToast(d.message || this.t("update_started"), "success");
                setTimeout(() => {
                    window.location.reload();
                }, 3500);
            }).catch(e => {
                this.showToast(this.t("update_failed"), "error");
                this.isUpdating = false;
            });
        }
    });
});
