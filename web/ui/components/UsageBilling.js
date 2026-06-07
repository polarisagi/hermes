export default {
    name: 'usageBillingComponent',
    setup() {
        return {
            fpInstance: null,
            dataInterval: null,

            get selectedAccountLabel() {
                if (Alpine.store('global').selectedAccount === 'all') return Alpine.store('global').t('all_summary');
                const matched = Alpine.store('global').availableAccounts.find(a => a.value === Alpine.store('global').selectedAccount);
                return matched ? matched.label : Alpine.store('global').selectedAccount;
            },

            get groupedApiData() {
                const map = {};
                Alpine.store('global').apiData.forEach(r => {
                    const key = r.account;
                    if (!map[key]) {
                        map[key] = {
                            account: r.account, platforms: new Set(), balance: r.balance, limit_percent: r.limit_percent,
                            valid_from: r.valid_from, total_cost_usd: r.total_cost_usd, cycle_cost_usd: r.cycle_cost_usd,
                            period_cost_usd: 0, prompt_tokens: 0, completion_tokens: 0, success_count: 0, error_count: 0, breakdown: [],
                            platformDetails: []
                        };
                    }
                    map[key].platforms.add(r.platform);
                    map[key].period_cost_usd += r.period_cost_usd;
                    map[key].prompt_tokens += r.prompt_tokens;
                    map[key].completion_tokens += r.completion_tokens;
                    map[key].success_count += r.success_count;
                    map[key].error_count += r.error_count;
                    
                    let pd = map[key].platformDetails.find(p => p.platform === r.platform);
                    if (!pd) {
                        pd = { platform: r.platform, cost: 0 };
                        map[key].platformDetails.push(pd);
                    }
                    pd.cost += r.period_cost_usd;
                });
                const result = Object.values(map);
                result.forEach(acc => acc.platforms = Array.from(acc.platforms));
                return result.sort((a,b) => b.period_cost_usd - a.period_cost_usd);
            },

            get singleAccountDetails() {
                if (Alpine.store('global').selectedAccount === 'all') return [];
                const details = Alpine.store('global').apiData.filter(d => d.account === Alpine.store('global').selectedAccount);
                return details.sort((a, b) => b.period_cost_usd - a.period_cost_usd);
            },

            getUsagePercent(row) {
                if (!row.balance || row.balance <= 0) return 0;
                return (row.cycle_cost_usd / row.balance) * 100;
            },

            getRemainingPercent(row) {
                if (!row.balance) return 100;
                const remain = row.limit_percent - this.getUsagePercent(row);
                return Math.max(0, remain).toFixed(2);
            },

            getBarColor(row) {
                const usage = this.getUsagePercent(row);
                if (usage >= row.limit_percent) return 'progress-error';
                if (usage >= row.limit_percent * 0.85) return 'progress-warning';
                return 'progress-success';
            },

            getRemainingColor(row) {
                const remain = parseFloat(this.getRemainingPercent(row));
                if (remain <= 0) return 'text-error animate-pulse';
                if (remain <= row.limit_percent * 0.15) return 'text-warning';
                return 'text-success';
            },

            get aggregatedData() {
                let source = Alpine.store('global').apiData;
                if (Alpine.store('global').selectedAccount !== 'all') {
                    source = source.filter(d => d.account === Alpine.store('global').selectedAccount);
                }
                let tCost = 0, tPrompt = 0, tComp = 0, tErr = 0, tSucc = 0;
                source.forEach(row => {
                    tCost += row.period_cost_usd; tPrompt += row.prompt_tokens;
                    tComp += row.completion_tokens; tErr += row.error_count; tSucc += row.success_count;
                });
                let rate = 0;
                if (tSucc + tErr > 0) rate = ((tSucc / (tSucc + tErr)) * 100).toFixed(2);
                return { totalCost: tCost, totalPrompt: tPrompt, totalCompletion: tComp, totalError: tErr, totalSuccess: tSucc, successRate: rate };
            },

            async fetchData() {
                if (Alpine.store('global').currentTab !== 'usage_billing') return;
                try {
                    const res = await fetch(`/api/admin/usage_billing/stats?start=${Alpine.store('global').startDate}&end=${Alpine.store('global').endDate}`);
                    const json = await res.json();
                    Alpine.store('global').apiData = json.details || [];
                    const accSet = new Set(Alpine.store('global').apiData.map(d => d.account));
                    Alpine.store('global').availableAccounts = Array.from(accSet).map(a => ({ account: a, label: a, value: a }));
                    Alpine.store('global').concurrency = { active: json.active_count || 0, waiting: json.waiting_count || 0, max: json.max_limit || 0 };
                } catch (e) {
                    console.error(Alpine.store('global').t("dashboard_fetch_failed"), e);
                }
            },

            updateDateRange(start, end, presetName) {
                Alpine.store('global').startDate = start; 
                Alpine.store('global').endDate = end; 
                Alpine.store('global').activePreset = presetName;
                if (this.fpInstance) this.fpInstance.setDate([start, end]);
                this.fetchData();
            },

            formatDate(date) {
                const d = new Date(date);
                return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
            },

            setPreset(preset) {
                const today = new Date(); let start = new Date();
                if (preset === 'today') start = today;
                else if (preset === 'week') start.setDate(today.getDate() - 6);
                else if (preset === 'month') start = new Date(today.getFullYear(), today.getMonth(), 1);
                this.updateDateRange(this.formatDate(start), this.formatDate(today), preset);
            },

            init() {
                // Wait for DOM to render the x-show element
                setTimeout(() => {
                    this.fpInstance = flatpickr("#datePicker", {
                        mode: "range", dateFormat: "Y-m-d", locale: "zh",
                        onChange: (selectedDates) => {
                            if (selectedDates.length === 2) {
                                Alpine.store('global').activePreset = 'custom';
                                Alpine.store('global').startDate = this.formatDate(selectedDates[0]);
                                Alpine.store('global').endDate = this.formatDate(selectedDates[1]);
                                this.fetchData();
                            }
                        }
                    });
                }, 100);
                this.setPreset('today');
                this.dataInterval = setInterval(() => {
                    this.fetchData();
                }, 3000);
            },

            destroy() {
                if (this.dataInterval) clearInterval(this.dataInterval);
            }
        };
    },
    template: `
        <div x-show="$store.global.currentTab === 'usage_billing'" class="max-w-6xl mx-auto w-full">
            <div class="flex justify-between items-start mb-8 border-b border-base-300 pb-6">
                <div>
                    <h2 class="text-3xl font-bold" x-text="$store.global.t('tab_usage_billing_title')"></h2>
                    <p class="text-base-content/60 mt-2" x-text="$store.global.t('dashboard_subtitle')"></p>
                </div>
                <div class="flex gap-4">
                    <div class="card bg-base-100 shadow px-5 py-3 flex flex-col items-center">
                        <span class="text-[10px] text-base-content/50 uppercase font-bold tracking-wider mb-1" x-text="$store.global.t('processing')"></span>
                        <div class="flex items-baseline gap-1">
                            <span class="text-2xl font-mono text-success" x-text="$store.global.concurrency.active"></span>
                            <span class="text-xs text-base-content/50" x-text="'/ ' + $store.global.concurrency.max"></span>
                        </div>
                    </div>
                    <div class="card bg-base-100 shadow px-5 py-3 flex flex-col items-center"
                        :class="$store.global.concurrency.waiting > 0 ? 'border-warning/50 border' : ''">
                        <span class="text-[10px] text-base-content/50 uppercase font-bold tracking-wider mb-1" x-text="$store.global.t('waiting')"></span>
                        <div class="flex items-center gap-2">
                            <span class="text-2xl font-mono"
                                :class="$store.global.concurrency.waiting > 0 ? 'text-warning animate-pulse' : 'text-base-content/30'"
                                x-text="$store.global.concurrency.waiting">
                            </span>
                        </div>
                    </div>
                    <div class="flex flex-col justify-end">
                        <select name="store_global_selectedAccount" x-model="$store.global.selectedAccount" class="select select-bordered select-sm w-full max-w-xs">
                            <option value="all" x-text="$store.global.t('view_all_protocols')"></option>
                            <template x-for="acc in $store.global.availableAccounts" :key="acc.value">
                                <option :value="acc.value" x-text="acc.label"></option>
                            </template>
                        </select>
                    </div>
                </div>
            </div>

            <div class="card bg-base-100 shadow p-4 mb-8 flex flex-row flex-wrap gap-4 items-center justify-between">
                <div class="join">
                    <button @click="setPreset('today')" :class="$store.global.activePreset === 'today' ? 'btn-active' : ''" class="btn btn-sm join-item" x-text="$store.global.t('filter_today')"></button>
                    <button @click="setPreset('week')" :class="$store.global.activePreset === 'week' ? 'btn-active' : ''" class="btn btn-sm join-item" x-text="$store.global.t('filter_7days')"></button>
                    <button @click="setPreset('month')" :class="$store.global.activePreset === 'month' ? 'btn-active' : ''" class="btn btn-sm join-item" x-text="$store.global.t('filter_month')"></button>
                </div>
                <div class="flex items-center gap-3">
                    <span class="text-base-content/60 text-sm" x-text="$store.global.t('custom_range')"></span>
                    <input type="text" id="datePicker" :placeholder="$store.global.t('select_date_range')" class="input input-bordered input-sm w-64 text-center">
                </div>
            </div>

            <div class="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
                <div class="card bg-base-100 shadow border-l-4 border-success">
                    <div class="card-body p-6">
                        <p class="text-base-content/60 text-sm mb-1" x-text="$store.global.t('estimated_cost')"></p>
                        <h3 class="text-3xl font-bold">$ <span x-text="$store.global.formatNum(aggregatedData.totalCost)"></span></h3>
                    </div>
                </div>
                <div class="card bg-base-100 shadow border-l-4 border-info">
                    <div class="card-body p-6">
                        <p class="text-base-content/60 text-sm mb-1" x-text="$store.global.t('prompt_tokens')"></p>
                        <h3 class="text-2xl font-bold text-info" x-text="$store.global.formatToken(aggregatedData.totalPrompt)"></h3>
                    </div>
                </div>
                <div class="card bg-base-100 shadow border-l-4 border-secondary">
                    <div class="card-body p-6">
                        <p class="text-base-content/60 text-sm mb-1" x-text="$store.global.t('completion_tokens')"></p>
                        <h3 class="text-2xl font-bold text-secondary" x-text="$store.global.formatToken(aggregatedData.totalCompletion)"></h3>
                    </div>
                </div>
                <div class="card bg-base-100 shadow border-l-4" :class="$store.global.successRateColor(aggregatedData.successRate)">
                    <div class="card-body p-6">
                        <p class="text-base-content/60 text-sm mb-1" x-text="$store.global.t('success_rate')"></p>
                        <div class="flex items-end gap-2">
                            <h3 class="text-2xl font-bold"><span x-text="aggregatedData.successRate"></span>%</h3>
                        </div>
                    </div>
                </div>
            </div>

            <template x-if="$store.global.selectedAccount === 'all'">
                <div class="card bg-base-100 shadow overflow-x-auto">
                    <table class="table table-zebra w-full">
                        <thead>
                            <tr>
                                <th x-text="$store.global.t('matrix_header_node')"></th>
                                <th class="text-right" x-text="$store.global.t('matrix_header_cost')"></th>
                                <th class="text-center w-1/4" x-text="$store.global.t('matrix_header_level')"></th>
                                <th class="text-right" x-text="$store.global.t('matrix_header_tokens')"></th>
                                <th class="text-center" x-text="$store.global.t('matrix_header_status')"></th>
                            </tr>
                        </thead>
                        <tbody>
                            <template x-for="row in groupedApiData" :key="row.account">
                                <tr>
                                    <td>
                                        <div class="font-medium text-primary text-lg mb-2" x-text="row.account"></div>
                                        <div class="flex flex-col gap-2">
                                            <template x-for="pd in row.platformDetails" :key="pd.platform">
                                                <div class="badge badge-outline p-3 gap-2 w-full justify-between">
                                                    <span :class="pd.platform === 'openai' ? 'text-primary' : 'text-success'" class="uppercase text-xs font-bold" x-text="pd.platform + ' ' + $store.global.t('protocol_suffix')"></span>
                                                    <span class="text-success font-mono">$\x3Cspan x-text="$store.global.formatNum(pd.cost)">\x3C/span></span>
                                                </div>
                                            </template>
                                        </div>
                                    </td>
                                    <td class="text-right font-bold text-success align-top pt-5">
                                        $ <span x-text="$store.global.formatNum(row.period_cost_usd)"></span>
                                    </td>
                                    <td class="align-top pt-5">
                                        <template x-if="row.balance > 0">
                                            <div>
                                                <div class="flex justify-between text-[10px] mb-1">
                                                    <span class="text-base-content/50" x-text="$store.global.t('from_date') + ' ' + $store.global.formatShortDate(row.valid_from)"></span>
                                                    <span :class="getRemainingColor(row)" class="font-semibold" x-text="$store.global.t('remaining') + ' ' + getRemainingPercent(row) + '%'"></span>
                                                </div>
                                                <progress class="progress w-full" :class="getBarColor(row)" :value="Math.min(getUsagePercent(row), 100)" max="100"></progress>
                                                <div class="text-[10px] text-base-content/50 mt-1 flex justify-between">
                                                    <span x-text="$store.global.t('this_cycle') + ' $' + $store.global.formatNum(row.cycle_cost_usd)"></span>
                                                    <span x-text="$store.global.t('total_balance') + ' $' + $store.global.formatNum(row.balance)"></span>
                                                </div>
                                            </div>
                                        </template>
                                        <template x-if="!(row.balance > 0)">
                                            <div class="badge badge-ghost w-full" x-text="$store.global.t('no_limit')"></div>
                                        </template>
                                    </td>
                                    <td class="text-right align-top pt-5" x-text="$store.global.formatToken(row.prompt_tokens + row.completion_tokens)"></td>
                                    <td class="text-center align-top pt-5">
                                        <span class="badge badge-success badge-sm mr-1">√ <span x-text="row.success_count"></span></span>
                                        <span class="badge badge-error badge-sm">x <span x-text="row.error_count"></span></span>
                                    </td>
                                </tr>
                            </template>
                            <template x-if="groupedApiData.length === 0">
                                <tr>
                                    <td colspan="5" class="text-center py-8 text-base-content/50" x-text="$store.global.t('no_data_range')"></td>
                                </tr>
                            </template>
                        </tbody>
                    </table>
                </div>
            </template>

            <template x-if="$store.global.selectedAccount !== 'all'">
                <div class="card bg-base-100 shadow overflow-hidden">
                    <div class="bg-base-200 px-6 py-4 flex items-center justify-between">
                        <h3 class="text-lg font-semibold text-primary">
                            <span class="text-base-content/50 text-sm font-normal mr-2" x-text="$store.global.t('current_filtered_node')"></span>
                            <span x-text="selectedAccountLabel"></span>
                        </h3>
                    </div>
                    <div class="overflow-x-auto">
                        <table class="table table-zebra w-full">
                            <thead>
                                <tr>
                                    <th x-text="$store.global.t('detail_header_platform')"></th>
                                    <th x-text="$store.global.t('detail_header_client')"></th>
                                    <th x-text="$store.global.t('detail_header_method')"></th>
                                    <th class="text-right" x-text="$store.global.t('detail_header_cost')"></th>
                                    <th class="text-right" x-text="$store.global.t('prompt_output')"></th>
                                    <th class="text-center" x-text="$store.global.t('matrix_header_status')"></th>
                                </tr>
                            </thead>
                            <tbody>
                                <template x-for="row in singleAccountDetails" :key="row.platform + row.client + row.method">
                                    <tr>
                                        <td><span :class="row.platform === 'openai' ? 'text-primary' : 'text-success'" class="font-bold uppercase text-xs" x-text="row.platform"></span></td>
                                        <td class="font-medium" x-text="row.client"></td>
                                        <td class="text-info font-mono text-xs" x-text="row.method"></td>
                                        <td class="text-right font-bold text-success">$ <span x-text="$store.global.formatNum(row.period_cost_usd)"></span></td>
                                        <td class="text-right text-base-content/60 text-xs"><span x-text="$store.global.formatToken(row.prompt_tokens)"></span> / <span x-text="$store.global.formatToken(row.completion_tokens)"></span></td>
                                        <td class="text-center">
                                            <span class="badge badge-success badge-sm mr-1">√ <span x-text="row.success_count"></span></span>
                                            <span class="badge badge-error badge-sm">x <span x-text="row.error_count"></span></span>
                                        </td>
                                    </tr>
                                </template>
                            </tbody>
                        </table>
                    </div>
                </div>
            </template>
        </div>
    `
};
