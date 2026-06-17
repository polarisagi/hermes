export default {
    name: 'aiAccountsComponent',
    setup() {
        return {
            nodeModal: { show: false, isEdit: false },
            modelsModal: { show: false, nodeId: 0, nodeName: '', nodeProvider: '' },
            channelModels: [],
            addModelModal: { show: false },
            addModelForm: { model_id: '', capability_tier: 'smart' },
            sysProviders: [],
            sysEndpoints: [],
            availableEndpoints: [],
            selectedEndpoint: null,
            nodeForm: {
                id: 0, provider: 'openai', name: '', credentials: '', project_id: '', location: 'global', endpoints: [],
                priority: 10, limit_percent: 90.0, balance: 0.0, min_request_interval_sec: 0, concurrency: 0,
                valid_from: '', valid_to: '', status: 1
            },
            
            toDatetimeLocal(dt) {
                if (!dt) return '';
                dt = dt.trim();
                dt = dt.replace(/Z$/, '').replace(/[+-]\\d{2}:\\d{2}$/, '');
                if (dt.length === 10) return dt + 'T00:00:00';
                return dt.replace(' ', 'T');
            },
            fromDatetimeLocal(dt) { return dt ? dt.trim().replace('T', ' ') : ''; },
            todayPrefix() {
                const d = new Date();
                const pad = n => String(n).padStart(2, '0');
                return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}`;
            },

            usagePercent(node) {
                if (!node.balance || node.balance <= 0) return 0;
                return ((node.used_amount || 0) / node.balance) * 100;
            },

            async fetchNodes() {
                if (Alpine.store('global').currentTab !== 'ai_accounts' && Alpine.store('global').currentTab !== 'smart_routing') return;
                try {
                    const res = await fetch('/api/admin/ai_accounts');
                    let nodes = await res.json() || [];
                    nodes = nodes.map(n => {
                        // find endpoint
                        const ep = this.sysEndpoints.find(e => e.provider_id === n.provider_id);
                        n.provider = n.provider_id;
                        n.concurrency = n.concurrency_limit || 0;
                        n.min_request_interval_sec = n.min_interval_sec || 0;
                        return n;
                    });
                    Alpine.store('global').nodes = nodes;
                } catch (e) { console.error(e) }
            },

            async fetchAllModels() {
                try {
                    const res = await fetch('/api/admin/models');
                    const json = await res.json() || [];
                    Alpine.store('global').allModels = Array.isArray(json) ? json : [];
                } catch (e) { console.error(e); }
            },

            async fetchSysProviders() {
                try {
                    const res = await fetch('/api/admin/sys_providers');
                    const data = await res.json();
                    if (data && data.providers) {
                        this.sysProviders = data.providers;
                        this.sysEndpoints = data.endpoints;
                    }
                } catch (e) { console.error("Failed to fetch sys_providers", e); }
            },

            // ── Model Sub-Panel Methods ────────────────────────────────────────

            getModelsForChannel(nodeId) {
                const all = Alpine.store('global').allModels || [];
                return all.filter(m => m.user_provider_id === nodeId);
            },

            getTierBadgeClass(tier) {
                const map = { smart: 'badge-warning', fast: 'badge-info' };
                return map[tier] || 'badge-ghost';
            },

            openModelsModal(node) {
                this.modelsModal = {
                    show: true,
                    nodeId: node.id,
                    nodeName: node.name,
                    nodeProvider: node.provider
                };
                this.channelModels = this.getModelsForChannel(node.id);
            },

            async changeModelTier(modelId, newTier) {
                const gStore = Alpine.store('global');
                try {
                    const res = await fetch('/api/admin/models', {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ id: modelId, capability_tier: newTier })
                    });
                    if (res.ok) {
                        await this.fetchAllModels();
                        this.channelModels = this.getModelsForChannel(this.modelsModal.nodeId);
                        gStore.showToast('梯队已更新');
                    } else {
                        gStore.showToast('更新失败', 'error');
                    }
                } catch (e) { gStore.showToast('网络错误', 'error'); }
            },

            async removeModel(modelId) {
                const gStore = Alpine.store('global');
                if (!confirm('确定要从该渠道中移除此模型吗？')) return;
                try {
                    const res = await fetch(`/api/admin/models?id=${modelId}`, { method: 'DELETE' });
                    if (res.ok) {
                        await this.fetchAllModels();
                        this.channelModels = this.getModelsForChannel(this.modelsModal.nodeId);
                        gStore.showToast('已移除');
                    } else {
                        gStore.showToast('移除失败', 'error');
                    }
                } catch (e) { gStore.showToast('网络错误', 'error'); }
            },

            async addModelToChannel() {
                const gStore = Alpine.store('global');
                const modelId = (this.addModelForm.model_id || '').trim();
                if (!modelId) {
                    gStore.showToast('模型名称不能为空', 'error');
                    return;
                }
                try {
                    const res = await fetch('/api/admin/models', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({
                            user_provider_id: this.modelsModal.nodeId,
                            model_id: modelId,
                            capability_tier: this.addModelForm.capability_tier,
                            display_name: modelId
                        })
                    });
                    if (res.ok) {
                        this.addModelModal.show = false;
                        this.addModelForm = { model_id: '', capability_tier: 'smart' };
                        await this.fetchAllModels();
                        this.channelModels = this.getModelsForChannel(this.modelsModal.nodeId);
                        gStore.showToast('模型已添加');
                    } else {
                        const err = await res.text();
                        gStore.showToast('添加失败: ' + err, 'error');
                    }
                } catch (e) { gStore.showToast('网络错误', 'error'); }
            },

            async autoImportModels() {
                const gStore = Alpine.store('global');
                try {
                    const res = await fetch('/api/admin/ai_accounts/auto_import', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({
                            node_id: this.modelsModal.nodeId,
                            provider_id: this.modelsModal.nodeProvider
                        })
                    });
                    if (res.ok) {
                        await this.fetchAllModels();
                        this.channelModels = this.getModelsForChannel(this.modelsModal.nodeId);
                        gStore.showToast('已自动同步系统内置模型');
                    } else {
                        const err = await res.text();
                        gStore.showToast('同步失败: ' + err, 'error');
                    }
                } catch (e) {
                    gStore.showToast('网络错误', 'error');
                }
            },

            openNodeModal(node = null) {
                if (node) {
                    node.provider = node.provider_id;
                    const ep = this.sysEndpoints.find(e => e.provider_id === node.provider_id);
                    
                    const origCreds = node.auth_credentials || {};

                    const p = this.sysProviders.find(sp => sp.provider_id === node.provider_id);
                    const defaultTimeout = p ? p.default_timeout_sec : 600;

                    this.nodeForm = {
                        ...node,
                        
                        credentials: '',
                        project_id: origCreds.project_id || '',
                        location: origCreds.region || 'global',
                        limit_percent: node.limit_percent !== undefined ? node.limit_percent : 90.0,
                        timeout_sec: (node.timeout_sec && node.timeout_sec > 0) ? node.timeout_sec : defaultTimeout,
                        valid_from: this.toDatetimeLocal(node.valid_from),
                        valid_to: this.toDatetimeLocal(node.valid_to),
                    };
                    this.nodeModal = { show: true, isEdit: true };
                } else {
                    const today = this.todayPrefix();
                    this.nodeForm = {
                        id: 0, provider: 'openai', name: '', credentials: '', project_id: '', location: 'global', base_url: '',
                        priority: 10, limit_percent: 90.0, balance: 0.0, min_request_interval_sec: 0, concurrency: 0, timeout_sec: 600,
                        valid_from: `${today}T00:00:00`, valid_to: `2099-12-31T23:59:59`, status: 1
                    };
                    // Apply provider defaults for new node
                    const p = this.sysProviders.find(sp => sp.provider_id === this.nodeForm.provider);
                    if (p) {
                        this.nodeForm.timeout_sec = p.default_timeout_sec || 600;
                        this.nodeForm.concurrency = p.default_concurrency || 0;
                    }
                    this.nodeModal = { show: true, isEdit: false };
                }
            },

            onProviderChange() {
                if (!this.nodeModal.isEdit) {
                    const p = this.sysProviders.find(sp => sp.provider_id === this.nodeForm.provider);
                    if (p) {
                        this.nodeForm.timeout_sec = p.default_timeout_sec || 600;
                        this.nodeForm.concurrency = p.default_concurrency || 0;
                    }
                }
            },

            async saveNode() {
                const form = this.nodeForm;
                const gStore = Alpine.store('global');
                if (!form.name || (!this.nodeModal.isEdit && !form.credentials && form.provider !== 'ollama')) {
                    gStore.showToast(gStore.t('err_empty_node'), 'error');
                    return;
                }
                if (form.provider === 'google' && !form.project_id) {
                    gStore.showToast(gStore.t('err_gcp_project'), 'error');
                    return;
                }
                if (form.priority < 0 || form.balance < 0 || form.limit_percent < 0) {
                    gStore.showToast(gStore.t('err_negative_numbers'), 'error');
                    return;
                }
                if (form.limit_percent > 100) {
                    gStore.showToast(gStore.t('err_limit_exceed'), 'error');
                    return;
                }
                if (form.concurrency < 0 || form.concurrency > 1000) {
                    gStore.showToast('并发限制必须在 0 到 1000 之间', 'error');
                    return;
                }
                if (form.timeout_sec < 1) {
                    gStore.showToast('超时时间必须大于 0', 'error');
                    return;
                }

                try {
                    const method = this.nodeModal.isEdit ? 'PUT' : 'POST';
                    
                    // Map form fields to backend expectations
                    let authCreds = {};
                    if (this.nodeModal.isEdit && !form.credentials) {
                        authCreds = { ...(form.auth_credentials || {}) };
                    } else if (this.selectedEndpoint) {
                        if (this.selectedEndpoint.auth_type === 'adc') {
                            try {
                                authCreds = JSON.parse(form.credentials);
                            } catch(e) {
                                authCreds.adc_json = form.credentials;
                            }
                        } else if (this.selectedEndpoint.auth_type !== 'none') {
                            authCreds.api_key = form.credentials;
                        }
                    }
                    if (this.selectedEndpoint) {
                        if (this.selectedEndpoint.required_credential_fields.includes('project_id')) authCreds.project_id = form.project_id;
                        if (this.selectedEndpoint.required_credential_fields.includes('region')) authCreds.region = form.location;
                    }
                    
                    const payload = {
                        ...form,
                        provider_id: form.provider,
                        auth_credentials: authCreds,
                        concurrency_limit: form.concurrency,
                        timeout_sec: form.timeout_sec,
                        min_interval_sec: form.min_request_interval_sec,
                        valid_from: this.fromDatetimeLocal(form.valid_from),
                        valid_to: this.fromDatetimeLocal(form.valid_to)
                    };
                    const res = await fetch('/api/admin/ai_accounts', {
                        method,
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(payload)
                    });
                    if (res.ok) {
                        gStore.showToast(this.nodeModal.isEdit ? gStore.t('node_updated') : gStore.t('node_added'));
                        this.nodeModal.show = false;
                        this.fetchNodes();
                    } else {
                        const err = await res.text();
                        gStore.showToast(gStore.t('save_failed') + ': ' + err, 'error');
                    }
                } catch(e) {
                    gStore.showToast(gStore.t('network_error'), 'error');
                }
            },

            async deleteNode(id) {
                const gStore = Alpine.store('global');
                if(!confirm(gStore.lang === 'zh' ? '确定要删除这个节点吗？此操作不可恢复。' : 'Are you sure you want to delete this node? This action cannot be undone.')) return;
                try {
                    const res = await fetch(`/api/admin/ai_accounts?id=${id}`, { method: 'DELETE' });
                    if (res.ok) {
                        gStore.showToast(gStore.t('node_deleted'));
                        this.fetchNodes();
                    } else {
                        gStore.showToast(gStore.t('delete_failed'), 'error');
                    }
                } catch(e) {
                    gStore.showToast(gStore.t('network_error'), 'error');
                }
            },

            startGoogleAuth() {
                const gStore = Alpine.store('global');
                const isLocal = window.location.hostname === '127.0.0.1' || window.location.hostname === 'localhost';
                if (!isLocal) {
                    alert(gStore.t("oauth_alert"));
                    return;
                }
                
                const receiveMessage = (event) => {
                    if (event.data && event.data.type === 'google_adc_auth' && event.data.data) {
                        this.nodeForm.credentials = event.data.data;
                        gStore.showToast(gStore.t('adc_filled'));
                        window.removeEventListener('message', receiveMessage);
                    }
                };
                window.addEventListener('message', receiveMessage, false);

                const width = 600;
                const height = 700;
                const left = Math.max(0, (window.innerWidth - width) / 2 + window.screenX);
                const top = Math.max(0, (window.innerHeight - height) / 2 + window.screenY);
                window.open('/api/admin/oauth/google/start', 'GoogleAuth', `width=${width},height=${height},top=${top},left=${left}`);
            },

            init() {
                this.fetchSysProviders();
                this.fetchNodes();
                this.fetchAllModels();

                
                // Function to update computed auth mode state
                const updateAuthModes = () => {
                    let eps = this.sysEndpoints.filter(m => m.provider_id === this.nodeForm.provider);
                    eps.sort((a, b) => {
                        if (a.api_protocol === 'openai' && b.api_protocol !== 'openai') return -1;
                        if (b.api_protocol === 'openai' && a.api_protocol !== 'openai') return 1;
                        return 0;
                    });
                    this.availableEndpoints = eps;
                    if (this.availableEndpoints.length > 0) {
                        this.selectedEndpoint = this.availableEndpoints[0];
                    } else {
                        this.selectedEndpoint = null;
                    }
                    
                    if (!this.nodeForm.endpoints) {
                        this.nodeForm.endpoints = [];
                    }

                    if (!this.nodeModal.isEdit) {
                        this.nodeForm.endpoints = this.availableEndpoints.map(e => ({
                            sys_endpoint_id: e.endpoint_id,
                            is_enabled: (this.nodeForm.provider === 'agent_platform' && e.api_protocol === 'anthropic') ? false : true,
                            custom_base_url: ''
                        }));
                    } else {
                        const existingMap = {};
                        this.nodeForm.endpoints.forEach(e => { existingMap[e.sys_endpoint_id] = e; });
                        this.nodeForm.endpoints = this.availableEndpoints.map(e => {
                            if (existingMap[e.endpoint_id]) {
                                return existingMap[e.endpoint_id];
                            }
                            return {
                                sys_endpoint_id: e.endpoint_id,
                                is_enabled: false,
                                custom_base_url: ''
                            };
                        });
                    }
                };

                this.$watch('nodeForm.provider', (newVal) => {
                    if (!this.nodeModal.isEdit) {
                        updateAuthModes();
                    }
                    if (!this.nodeModal.isEdit && newVal === 'vertex') {
                        this.nodeForm.concurrency = 1;
                    } else if (!this.nodeModal.isEdit && newVal !== 'vertex') {
                        this.nodeForm.concurrency = 0;
                    }
                    if (!this.nodeModal.isEdit) {
                        if (this.selectedEndpoint && this.selectedEndpoint.auth_type === 'none') {
                            this.nodeForm.credentials = '';
                        }
                    }
                });


                
                // Also trigger initial calculation when modal opens
                this.$watch('nodeModal.show', (newVal) => {
                    if (newVal) {
                        updateAuthModes();
                    }
                });
                
                this.$watch('$store.global.currentTab', (newTab) => {
                    if (newTab === 'ai_accounts') {
                        this.fetchNodes();
                        this.fetchAllModels();
                    }
                });
            }
        };
    },
    template: `
        <div x-show="$store.global.currentTab === 'ai_accounts'" class="max-w-6xl mx-auto w-full">
            <div class="flex justify-between items-center mb-6">
                <div>
                    <h2 class="text-3xl font-bold" x-text="$store.global.t('tab_ai_accounts_title')"></h2>
                    <p class="text-base-content/60 text-sm mt-2" x-text="$store.global.t('channels_subtitle')"></p>
                </div>
                <button @click="openNodeModal()" class="btn btn-primary shadow-lg shadow-primary/20">
                    <span class="text-lg">+</span> <span x-text="$store.global.t('btn_add_new_node')"></span>
                </button>
            </div>
            
            <div class="card bg-base-100 shadow overflow-x-auto">
                <table class="table table-zebra w-full">
                    <thead>
                        <tr>
                            <th x-text="$store.global.t('table_platform')"></th>
                            <th x-text="$store.global.t('node_name')"></th>
                            <th class="text-center" x-text="$store.global.t('table_pri_con')"></th>
                            <th x-text="$store.global.t('table_limit_usage')"></th>
                            <th x-text="$store.global.t('valid_range')"></th>
                            <th class="text-center">模型</th>
                            <th class="text-center w-20" x-text="$store.global.t('table_status')"></th>
                            <th class="text-right w-24" x-text="$store.global.t('actions')"></th>
                        </tr>
                    </thead>
                    <tbody>
                        <template x-for="node in $store.global.nodes" :key="node.id">
                            <tr>
                                <td>
                                    <span :class="$store.global.protocolBadge(node.provider)"
                                        class="badge badge-sm font-bold uppercase" x-text="$store.global.protocolLabel(node.provider)"></span>
                                </td>
                                <td class="font-medium" x-text="node.name"></td>
                                <td class="text-center text-xs space-y-0.5">
                                    <div class="font-bold font-mono text-sm" x-text="node.priority" title="优先级 (Priority)"></div>
                                    <div class="text-base-content/50 font-mono text-[11px]" x-text="node.concurrency === 0 ? '∞' : node.concurrency" title="并发限制 (Concurrency)"></div>
                                </td>
                                <td>
                                    <template x-if="node.balance > 0">
                                        <div class="space-y-1">
                                            <div class="flex items-center justify-between text-[10px]">
                                                <span class="text-base-content/50">$\x3Cspan x-text="$store.global.formatNum(node.used_amount || 0)">\x3C/span> / $\x3Cspan x-text="$store.global.formatNum(node.balance)">\x3C/span></span>
                                                <span :class="usagePercent(node) >= node.limit_percent ? 'text-error' : 'text-base-content/50'" x-text="usagePercent(node).toFixed(1) + '%'"></span>
                                            </div>
                                            <progress class="progress w-full" :class="usagePercent(node) >= node.limit_percent ? 'progress-error' : 'progress-success'" :value="Math.min(usagePercent(node), 100)" max="100"></progress>
                                        </div>
                                    </template>
                                    <template x-if="!(node.balance > 0)">
                                        <span class="text-base-content/50 text-xs" x-text="$store.global.t('no_limit_text')"></span>
                                    </template>
                                </td>
                                <td class="text-xs text-base-content/60">
                                    <template x-if="node.valid_from && node.valid_to">
                                        <div>
                                            <span x-text="$store.global.formatShortDate(node.valid_from)"></span><br><span class="text-base-content/30">~</span> <span x-text="$store.global.formatShortDate(node.valid_to)"></span>
                                        </div>
                                    </template>
                                    <template x-if="!(node.valid_from && node.valid_to)">
                                        <span class="text-base-content/30">-</span>
                                    </template>
                                </td>
                                <td class="min-w-56 p-2">
                                    <div @click="openModelsModal(node)"
                                         class="cursor-pointer group relative p-1.5 rounded-lg hover:bg-base-200 transition-colors border border-transparent hover:border-base-300 min-h-[36px] flex items-center">
                                        <div class="flex flex-wrap gap-1.5 items-center w-full pr-10">
                                            <template x-if="getModelsForChannel(node.id).length === 0">
                                                <span class="text-xs text-base-content/40 italic">暂无配置</span>
                                            </template>
                                            
                                            <template x-for="(m, index) in getModelsForChannel(node.id).slice(0, 3)" :key="m.id">
                                                <div class="badge badge-sm badge-outline gap-1" :class="getTierBadgeClass(m.capability_tier)">
                                                    <span class="text-[10px]" x-text="m.capability_tier === 'smart' ? '🏆' : (m.capability_tier === 'fast' ? '⚡' : '✸')"></span>
                                                    <span class="text-[10px]" x-text="m.model_id"></span>
                                                </div>
                                            </template>
                                            <template x-if="getModelsForChannel(node.id).length > 3">
                                                <div class="badge badge-sm badge-ghost text-[10px]">
                                                    +<span x-text="getModelsForChannel(node.id).length - 3"></span>
                                                </div>
                                            </template>
                                        </div>
                                        <div class="absolute right-2 top-1/2 -translate-y-1/2 opacity-0 group-hover:opacity-100 transition-opacity">
                                            <button class="btn btn-xs btn-primary btn-outline shadow-sm">
                                                <span>⚙</span>
                                            </button>
                                        </div>
                                    </div>
                                </td>
                                <td class="text-center">
                                    <template x-if="node.status === 1"><span class="badge badge-success badge-sm" x-text="$store.global.t('status_enabled_short')"></span></template>
                                    <template x-if="node.status === 0"><span class="badge badge-ghost badge-sm" x-text="$store.global.t('status_disabled_short')"></span></template>
                                    <template x-if="node.status === -1"><span class="badge badge-error badge-sm" x-text="$store.global.t('status_exhausted_short')"></span></template>
                                </td>
                                <td class="text-right space-x-2">
                                    <button @click="openNodeModal(node)" class="btn btn-ghost btn-xs text-info" x-text="$store.global.t('edit')"></button>
                                    <button @click="deleteNode(node.id)" class="btn btn-ghost btn-xs text-error" x-text="$store.global.t('delete')"></button>
                                </td>
                            </tr>
                        </template>
                        <template x-if="$store.global.nodes.length === 0">
                            <tr>
                                <td colspan="8" class="text-center py-8 text-base-content/50" x-text="$store.global.t('no_nodes')"></td>
                            </tr>
                        </template>
                    </tbody>
                </table>
            </div>

            <!-- Node Editor Modal -->
            <dialog class="modal" :class="nodeModal.show ? 'modal-open' : ''">
                <div class="modal-box w-11/12 max-w-3xl">
                    <button class="btn btn-sm btn-circle btn-ghost absolute right-2 top-2" @click="nodeModal.show = false">✕</button>
                    <h3 class="font-bold text-lg mb-6" x-text="nodeModal.isEdit ? $store.global.t('edit_node') : $store.global.t('add_new_node')"></h3>
                    
                    <div class="space-y-6">
                        <!-- 区块 1: 基本信息 -->
                        <div class="bg-base-200 p-4 rounded-xl space-y-4 border border-base-300">
                            <h4 class="text-xs font-bold text-base-content/50 uppercase tracking-wider" x-text="$store.global.t('section_basic')"></h4>
                            <div class="grid grid-cols-2 gap-4">

                                <label class="form-control w-full">
                                    <div class="label"><span class="label-text font-medium">大模型厂商 <span class="text-error">*</span></span></div>
                                    <select name="nodeForm_provider" x-model="nodeForm.provider" @change="onProviderChange()" class="select select-bordered select-sm w-full">
                                        <template x-for="p in sysProviders" :key="p.provider_id">
                                            <option :value="p.provider_id" x-text="p.provider_name"></option>
                                        </template>
                                    </select>
                                </label>

                            </div>
                            <div class="grid grid-cols-1 gap-4">
                                <label class="form-control w-full">
                                    <div class="label"><span class="label-text font-medium"><span x-text="$store.global.t('node_name_req')"></span> <span class="text-error">*</span></span></div>
                                    <input name="nodeForm_name" x-model="nodeForm.name" type="text" :placeholder="$store.global.t('placeholder_node_name')" class="input input-bordered input-sm w-full">
                                </label>
                            </div>
                            

                            
                            <label class="form-control w-full">
                                <div class="label">
                                    <span class="label-text font-medium">
                                        <template x-if="selectedEndpoint && selectedEndpoint.auth_type === 'adc'"><span>ADC JSON</span></template>
                                        <template x-if="!selectedEndpoint || selectedEndpoint.auth_type !== 'adc'"><span>API Key</span></template>
                                        
                                        <template x-if="selectedEndpoint && selectedEndpoint.auth_type !== 'none'"><span class="text-error">*</span></template>
                                        
                                        <!-- 动态提示词 -->
                                        <template x-if="selectedEndpoint && selectedEndpoint.auth_type === 'adc'"><span class="text-base-content/50 text-xs ml-1 font-normal" x-text="$store.global.t('hint_adc_paste')"></span></template>
                                        <template x-if="selectedEndpoint && selectedEndpoint.auth_type === 'header'"><span class="text-base-content/50 text-xs ml-1 font-normal" x-text="'(放在 ' + selectedEndpoint.auth_header + ' 请求头中)'"></span></template>
                                        <template x-if="selectedEndpoint && selectedEndpoint.auth_type === 'bearer'"><span class="text-base-content/50 text-xs ml-1 font-normal" x-text="$store.global.t('hint_sk_bearer')"></span></template>
                                        <template x-if="selectedEndpoint && selectedEndpoint.auth_type === 'none'"><span class="text-base-content/50 text-xs ml-1 font-normal">通常无需验证，留空即可</span></template>
                                    </span>
                                    <template x-if="selectedEndpoint && selectedEndpoint.auth_type === 'adc'">
                                        <button @click="startGoogleAuth" class="btn btn-xs btn-outline btn-info">🔑 <span x-text="$store.global.t('btn_oauth_auto')"></span></button>
                                    </template>
                                </div>
                                
                                <template x-if="selectedEndpoint && selectedEndpoint.auth_type === 'adc'">
                                    <textarea name="nodeForm_credentials" x-model="nodeForm.credentials" rows="3" :placeholder="nodeModal.isEdit ? $store.global.t('placeholder_adc_edit') : $store.global.t('placeholder_adc_new')" class="textarea textarea-bordered font-mono text-xs w-full"></textarea>
                                </template>
                                <template x-if="!selectedEndpoint || selectedEndpoint.auth_type !== 'adc'">
                                    <input name="nodeForm_credentials" x-model="nodeForm.credentials" type="password" :placeholder="nodeModal.isEdit ? $store.global.t('placeholder_key_edit') : $store.global.t('placeholder_key_new')" class="input input-bordered input-sm w-full">
                                </template>
                            </label>
                            
                            <template x-if="$store.global.proMode">
                                <div class="grid grid-cols-2 gap-4">
                                    <label class="form-control w-full">
                                        <div class="label"><span class="label-text" x-text="$store.global.t('priority')"></span></div>
                                        <input name="nodeForm_priority" x-model.number="nodeForm.priority" type="number" min="0" class="input input-bordered input-sm w-full">
                                        <div class="label"><span class="label-text-alt text-base-content/50" x-text="$store.global.t('priority_hint')"></span></div>
                                    </label>
                                    <label class="form-control w-full">
                                        <div class="label"><span class="label-text" x-text="$store.global.t('min_interval_label')"></span></div>
                                        <input name="nodeForm_min_request_interval_sec" x-model.number="nodeForm.min_request_interval_sec" type="number" min="0" class="input input-bordered input-sm w-full">
                                        <div class="label"><span class="label-text-alt text-base-content/50" x-text="$store.global.t('min_interval_hint')"></span></div>
                                    </label>
                                </div>
                            </template>
                            <template x-if="$store.global.proMode">
                                <div class="grid grid-cols-2 gap-4">
                                    <label class="form-control w-full">
                                        <div class="label"><span class="label-text" x-text="$store.global.t('status')"></span></div>
                                        <select name="nodeForm_status" x-model.number="nodeForm.status" class="select select-bordered select-sm w-full">
                                            <option value="1" x-text="$store.global.t('status_option_enable')"></option>
                                            <option value="0" x-text="$store.global.t('status_option_disable')"></option>
                                            <option value="-1" x-text="$store.global.t('status_option_exhaust')"></option>
                                        </select>
                                    </label>
                                    <label class="form-control w-full">
                                        <div class="label"><span class="label-text">Concurrency (并发限制)</span></div>
                                        <input name="nodeForm_concurrency" x-model.number="nodeForm.concurrency" type="number" min="0" max="1000" class="input input-bordered input-sm w-full">
                                        <div class="label"><span class="label-text-alt text-base-content/50">0 为无限制，上限 1000</span></div>
                                    </label>
                                <div class="grid grid-cols-2 gap-4 mt-4">
                                    <label class="form-control w-full">
                                        <div class="label"><span class="label-text" x-text="$store.global.lang === 'zh' ? 'Timeout (超时时间)' : 'Timeout (sec)'"></span></div>
                                        <input name="nodeForm_timeout_sec" x-model.number="nodeForm.timeout_sec" type="number" min="1" class="input input-bordered input-sm w-full">
                                        <div class="label"><span class="label-text-alt text-base-content/50" x-text="$store.global.lang === 'zh' ? '请求超时秒数 (推理模型推荐 600)' : 'Request timeout in seconds (600 recommended)'"></span></div>
                                    </label>
                                </div>
                            </template>
                        </div>

                        <!-- 区块 2: 供应商配置 -->
                        <div class="bg-base-200 p-4 rounded-xl space-y-4 border border-base-300">
                            <h4 class="text-xs font-bold text-base-content/50 uppercase tracking-wider" x-text="$store.global.t('section_provider')"></h4>
                            <template x-if="selectedEndpoint && (selectedEndpoint.required_credential_fields.includes('project_id') || selectedEndpoint.required_credential_fields.includes('region'))">
                                <div class="grid grid-cols-2 gap-4">
                                    <template x-if="selectedEndpoint.required_credential_fields.includes('project_id')">
                                        <label class="form-control w-full">
                                            <div class="label"><span class="label-text font-medium"><span x-text="$store.global.t('gcp_project_id')"></span> <span class="text-error">*</span></span></div>
                                            <input name="nodeForm_project_id" x-model="nodeForm.project_id" type="text" placeholder="your-gcp-project-id" class="input input-bordered input-sm w-full">
                                        </label>
                                    </template>
                                    <template x-if="selectedEndpoint.required_credential_fields.includes('region')">
                                        <label class="form-control w-full">
                                            <div class="label"><span class="label-text" x-text="$store.global.t('gcp_location')"></span></div>
                                            <input name="nodeForm_location" x-model="nodeForm.location" type="text" placeholder="global" class="input input-bordered input-sm w-full">
                                            <div class="label"><span class="label-text-alt text-base-content/50" x-text="$store.global.t('hint_location')"></span></div>
                                        </label>
                                    </template>
                                </div>
                            </template>
                            <template x-if="$store.global.proMode && nodeForm.endpoints.length > 0">
                                <div class="space-y-3 mt-4 border-t border-base-300 pt-4">
                                    <div class="flex items-center justify-between">
                                        <h5 class="text-xs font-bold text-base-content/70">协议端点控制</h5>
                                        <div class="tooltip tooltip-left" data-tip="您可以独立开启/关闭特定的底层协议，或为某个协议指定单独的代理中转地址。">
                                            <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-base-content/40 cursor-help" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
                                        </div>
                                    </div>
                                    <div class="space-y-2">
                                        <template x-for="(ep, index) in nodeForm.endpoints" :key="ep.sys_endpoint_id">
                                            <div class="flex flex-col gap-2 p-3 bg-base-100/50 rounded-lg border border-base-200">
                                                <div class="flex items-center justify-between">
                                                    <label class="cursor-pointer label py-0 gap-2 justify-start">
                                                        <input type="checkbox" class="toggle toggle-sm toggle-primary" x-model="ep.is_enabled">
                                                        <span class="label-text font-medium" x-text="availableEndpoints.find(e => e.endpoint_id === ep.sys_endpoint_id)?.display_name || ep.sys_endpoint_id"></span>
                                                    </label>
                                                    <span class="badge badge-sm badge-ghost font-mono text-[10px]" x-text="availableEndpoints.find(e => e.endpoint_id === ep.sys_endpoint_id)?.api_protocol"></span>
                                                </div>
                                                <div class="form-control w-full" x-show="ep.is_enabled" x-transition>
                                                    <input type="text" x-model="ep.custom_base_url" :placeholder="availableEndpoints.find(e => e.endpoint_id === ep.sys_endpoint_id)?.default_base_url" class="input input-bordered input-sm w-full text-xs font-mono">
                                                </div>
                                            </div>
                                        </template>
                                    </div>
                                </div>
                            </template>
                            <template x-if="!$store.global.proMode && selectedEndpoint && selectedEndpoint.api_protocol === 'local' && nodeForm.endpoints.length > 0">
                                <label class="form-control w-full">
                                    <div class="label"><span class="label-text" x-text="$store.global.t('base_url_optional')"></span></div>
                                    <input type="text" x-model="nodeForm.endpoints[0].custom_base_url" :placeholder="selectedEndpoint.default_base_url" class="input input-bordered input-sm w-full font-mono">
                                    <div class="label"><span class="label-text-alt text-base-content/50" x-text="$store.global.t('hint_custom_endpoint')"></span></div>
                                </label>
                            </template>
                        </div>

                        <!-- 区块 3: 计费与有效期 -->
                        <template x-if="$store.global.proMode">
                            <div class="bg-base-200 p-4 rounded-xl space-y-4 border border-base-300">
                                <h4 class="text-xs font-bold text-base-content/50 uppercase tracking-wider" x-text="$store.global.t('section_billing_validity')"></h4>
                                <div class="grid grid-cols-2 gap-4">
                                    <label class="form-control w-full">
                                        <div class="label"><span class="label-text" x-text="$store.global.t('label_total_balance')"></span></div>
                                        <input name="nodeForm_balance" x-model.number="nodeForm.balance" type="number" min="0" step="0.01" placeholder="0.00" class="input input-bordered input-sm w-full">
                                        <div class="label"><span class="label-text-alt text-base-content/50" x-text="$store.global.t('hint_unlimited')"></span></div>
                                    </label>
                                    <label class="form-control w-full">
                                        <div class="label"><span class="label-text" x-text="$store.global.t('label_limit_percent')"></span></div>
                                        <input name="nodeForm_limit_percent" x-model.number="nodeForm.limit_percent" type="number" min="0" max="100" step="0.1" class="input input-bordered input-sm w-full">
                                        <div class="label"><span class="label-text-alt text-base-content/50" x-text="$store.global.t('hint_limit_percent')"></span></div>
                                    </label>
                                    <label class="form-control w-full">
                                        <div class="label"><span class="label-text" x-text="$store.global.t('label_valid_from')"></span></div>
                                        <input name="nodeForm_valid_from" x-model="nodeForm.valid_from" type="datetime-local" step="1" class="input input-bordered input-sm w-full">
                                    </label>
                                    <label class="form-control w-full">
                                        <div class="label"><span class="label-text" x-text="$store.global.t('label_valid_to')"></span></div>
                                        <input name="nodeForm_valid_to" x-model="nodeForm.valid_to" type="datetime-local" step="1" class="input input-bordered input-sm w-full">
                                        <div class="label"><span class="label-text-alt text-base-content/50" x-text="$store.global.t('hint_expire_auto')"></span></div>
                                    </label>
                                </div>
                            </div>
                        </template>
                    </div>
                    
                    <div class="modal-action mt-6">
                        <button class="btn" @click="nodeModal.show = false" x-text="$store.global.t('cancel')"></button>
                        <button class="btn btn-primary shadow-lg shadow-primary/20" @click="saveNode()" x-text="$store.global.t('btn_save_simple')"></button>
                    </div>
                </div>
            </dialog>
            <!-- Models Management Modal -->
            <dialog class="modal" :class="modelsModal.show ? 'modal-open' : ''">
                <div class="modal-box w-11/12 max-w-2xl">
                    <button class="btn btn-sm btn-circle btn-ghost absolute right-2 top-2"
                            @click="modelsModal.show = false">✕</button>

                    <h3 class="font-bold text-lg mb-1">
                        <span x-text="modelsModal.nodeName"></span>
                        <span class="text-base-content/40 font-normal text-sm ml-2">模型列表</span>
                    </h3>
                    <p class="text-xs text-base-content/50 mb-4">渠道内所有可用模型及其能力梯队。创建渠道时自动导入，可手动调整梯队。</p>

                    <!-- Model list table -->
                    <div class="overflow-x-auto rounded-xl border border-base-300">
                        <table class="table table-sm table-zebra w-full">
                            <thead>
                                <tr>
                                    <th>模型 ID</th>
                                    <th class="text-center">能力梯队</th>
                                    <th class="text-right">操作</th>
                                </tr>
                            </thead>
                            <tbody>
                                <template x-for="m in channelModels" :key="m.id">
                                    <tr>
                                        <td>
                                            <div class="font-mono text-sm font-semibold" x-text="m.model_id"></div>
                                            <div class="text-xs text-base-content/40" x-text="m.display_name !== m.model_id ? m.display_name : ''"></div>
                                        </td>
                                        <td class="text-center">
                                            <div class="flex items-center justify-center gap-1">
                                                <button @click="changeModelTier(m.id, 'smart')"
                                                        :class="m.capability_tier === 'smart' ? 'badge-warning' : 'badge-ghost opacity-40 hover:opacity-80'"
                                                        class="badge badge-sm cursor-pointer transition-all" title="旗舰型">🏆</button>
                                                <button @click="changeModelTier(m.id, 'fast')"
                                                        :class="m.capability_tier === 'fast' ? 'badge-info' : 'badge-ghost opacity-40 hover:opacity-80'"
                                                        class="badge badge-sm cursor-pointer transition-all" title="极速型">⚡</button>
                                            </div>
                                        </td>
                                        <td class="text-right">
                                            <button @click="removeModel(m.id)"
                                                    class="btn btn-ghost btn-xs text-error">移除</button>
                                        </td>
                                    </tr>
                                </template>
                                <template x-if="channelModels.length === 0">
                                    <tr>
                                        <td colspan="3" class="text-center py-8 text-base-content/40">
                                            <div class="text-2xl mb-1">📭</div>
                                            <div class="text-sm">此渠道暂无模型</div>
                                            <div class="text-xs mt-1">请点击下方的自动同步或手动添加</div>
                                        </td>
                                    </tr>
                                </template>
                            </tbody>
                        </table>
                    </div>

                    <!-- Add model button (for local providers like Ollama) -->
                    <div class="mt-4 flex flex-col md:flex-row items-center justify-between gap-4">
                        <div class="text-xs text-base-content/40 w-full md:w-auto text-left leading-relaxed">
                            如果是 OpenAI/DeepSeek 等标准渠道，可点击自动同步内置模型。<br>Ollama 等本地渠道请手动添加。
                        </div>
                        <div class="flex gap-2 w-full md:w-auto justify-end">
                            <button @click="autoImportModels()"
                                    class="btn btn-sm btn-outline btn-info gap-1 flex-1 md:flex-none">
                                <span>🔄</span> 自动同步模型
                            </button>
                            <button @click="addModelModal.show = true"
                                    class="btn btn-sm btn-outline btn-success gap-1 flex-1 md:flex-none">
                                <span>+</span> 手动添加模型
                            </button>
                        </div>
                    </div>

                    <div class="modal-action mt-4">
                        <button class="btn" @click="modelsModal.show = false">关闭</button>
                    </div>
                </div>
                <div class="modal-backdrop" @click="modelsModal.show = false"></div>
            </dialog>

            <!-- Add Model Sub-Modal -->
            <dialog class="modal" :class="addModelModal.show ? 'modal-open' : ''">
                <div class="modal-box w-11/12 max-w-sm">
                    <button class="btn btn-sm btn-circle btn-ghost absolute right-2 top-2"
                            @click="addModelModal.show = false">✕</button>
                    <h3 class="font-bold text-base mb-4">手动添加模型</h3>
                    <div class="space-y-4">
                        <div class="form-control">
                            <div class="label pb-1"><span class="label-text font-medium">模型 ID *</span></div>
                            <input name="addModelForm_model_id" x-model="addModelForm.model_id"
                                   type="text" class="input input-bordered input-sm w-full font-mono"
                                   placeholder="e.g. qwen3:32b, llama4:70b" />
                        </div>
                        <div class="form-control">
                            <div class="label pb-2"><span class="label-text font-medium">能力梯队</span></div>
                            <div class="flex gap-2">
                                <button type="button" @click="addModelForm.capability_tier='smart'"
                                        :class="addModelForm.capability_tier==='smart'?'btn-warning':'btn-ghost'"
                                        class="btn btn-sm flex-1">🏆 旗舰</button>
                                <button type="button" @click="addModelForm.capability_tier='fast'"
                                        :class="addModelForm.capability_tier==='fast'?'btn-info':'btn-ghost'"
                                        class="btn btn-sm flex-1">⚡ 极速</button>
                            </div>
                        </div>
                    </div>
                    <div class="modal-action mt-5">
                        <button class="btn btn-sm" @click="addModelModal.show = false">取消</button>
                        <button class="btn btn-sm btn-success" @click="addModelToChannel()">确认添加</button>
                    </div>
                </div>
                <div class="modal-backdrop" @click="addModelModal.show = false"></div>
            </dialog>
        </div>
    `
};
