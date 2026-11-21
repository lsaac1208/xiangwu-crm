<template>
  <div class="page-container" :class="{ 'has-batch-selection': selectedLeadIDs.size }">
    <template v-if="!mobileLayout">
      <div class="kx-desktop-page lead-desktop-page">
        <div class="kx-desktop-container lead-desktop-container">
          <header class="kx-desktop-header">
            <div>
              <h1 class="kx-desktop-title">{{ pageTitle }}</h1>
              <p class="kx-desktop-subtitle">邀约、跟进与到访，清晰有序。</p>
            </div>
          </header>

		  <section class="workbench-band" aria-label="工作台摘要">
            <div v-for="item in workbenchSummaryItems" :key="item.key" class="workbench-metric" :class="{ danger: item.danger }">
              <span>{{ item.label }}</span>
              <strong :class="{ danger: item.danger }">{{ item.value }}</strong>
            </div>
            <button type="button" class="workbench-refresh" :disabled="workbenchLoading" aria-label="刷新工作台" title="刷新工作台" @click="loadWorkbench">
              <van-icon aria-hidden="true" name="replay" />
            </button>
          </section>

          <details v-if="workbench.tasks.length" class="workbench-task-panel">
            <summary><van-icon aria-hidden="true" name="warning-o" /><strong>{{ workbench.tasks.length }} 项异常待办</strong><span>需要关注</span><span class="workbench-task-disclosure">查看待办</span><van-icon aria-hidden="true" name="arrow-down" /></summary>
            <div class="workbench-task-list">
              <button
                v-for="task in workbench.tasks"
                :key="`${task.type}-${task.lead_id}-${task.appointment_id || 0}`"
                type="button"
                class="workbench-task"
                @click="openWorkbenchTask(task)"
              >
                <span class="workbench-task-dot" :class="task.severity" />
                <span>{{ workbenchTaskLabel(task) }}</span>
                <time v-if="task.due_at">{{ formatDate(task.due_at) }}</time>
                <van-icon aria-hidden="true" name="arrow" />
              </button>
            </div>
          </details>
          <button v-if="workbenchError" type="button" class="workbench-error" @click="loadWorkbench">工作台加载失败，点击重试</button>

          <section class="lead-list-surface" aria-label="线索列表">
          <section class="kx-desktop-toolbar lead-toolbar">
            <div class="kx-desktop-segment lead-status-segment">
              <button
                v-for="option in statusOptions"
                :key="option.name"
                v-show="option.name !== 'all' || !showAllPeopleSelect"
                type="button"
                :class="{ active: activeTab === option.name }"
                :aria-pressed="activeTab === option.name"
                @click="setActiveStatus(option.name)"
              >
                {{ option.title }}
              </button>
              <select v-if="showAllPeopleSelect" v-model="salesUserID" class="lead-all-select" aria-label="全部线索邀约师筛选" @change="onSalesFilterChange">
                <option value="">全部</option>
                <option v-for="item in salesUsers" :key="item.wecom_userid" :value="item.wecom_userid">{{ item.role === 'manager' ? '主管 · ' : '' }}{{ item.employee_name || item.wecom_userid }}</option>
              </select>
            </div>
            <div class="lead-toolbar-side">
              <div class="lead-view-actions">
			    <select v-model="selectedViewID" class="lead-toolbar-select" aria-label="保存的线索视图" @change="applySelectedView">
				  <option value="">当前视图</option>
				  <option v-for="view in savedViews" :key="view.id" :value="String(view.id)">{{ view.name }}</option>
			    </select>
			    <button type="button" class="lead-view-save" :disabled="savedViews.length >= 20" @click="openViewEditor(false)">保存视图</button>
          <button v-if="selectedViewID" type="button" class="lead-view-save lead-view-manage" @click="openViewEditor(true)">管理视图</button>
          <span v-if="savedViews.length >= 20" role="status">已达 20 个上限，可在管理视图中更新或删除</span>
              </div>
              <div class="lead-query-actions">
			    <select v-model="sortValue" class="lead-toolbar-select lead-sort-select" aria-label="线索排序" @change="onSortChange">
				  <option value="updated_at:desc">最近更新</option><option value="created_at:desc">最新创建</option><option value="created_at:asc">最早创建</option><option value="next_followup_due_at:asc">回访时间优先</option><option value="assigned_at:desc">最近分配</option><option value="converted_at:desc">最近成交</option>
			    </select>
                <div v-if="showInviterFilter" class="lead-date-dropdown">
                  <button type="button" class="lead-date-trigger" :class="{ active: startDate && endDate }" aria-controls="lead-date-popover" :aria-expanded="showDateFilter" @click="showDateFilter = !showDateFilter">
                    <van-icon aria-hidden="true" name="calendar-o" />
                    <span>{{ dateFilterLabel }}</span>
                    <van-icon aria-hidden="true" :name="showDateFilter ? 'arrow-up' : 'arrow-down'" />
                  </button>
                  <div v-if="showDateFilter" id="lead-date-popover" class="lead-date-popover">
                    <strong>按创建日期筛选</strong>
                    <label><span>开始</span><input v-model="startDate" type="date" /></label>
                    <label><span>结束</span><input v-model="endDate" type="date" /></label>
                    <div><button type="button" class="lead-filter-clear" @click="clearDateFilter">重置</button><button type="button" class="lead-filter-apply" @click="applyDateFilter">应用</button></div>
                  </div>
                </div>
                <van-search
                  v-model="keyword"
                  class="lead-desktop-search"
                  shape="round"
                  clearable
                  placeholder="搜索姓名、手机号、城市、来源"
                  @search="onSearch"
                  @clear="onSearch"
                  @update:model-value="onKeywordInput"
                />
                <button
                  type="button"
                  class="kx-desktop-button lead-refresh-button"
                  :disabled="loading"
                  @click="refreshLeads"
                >
                  <van-icon aria-hidden="true" name="replay" />
                  <span>{{ loading ? '刷新中' : '刷新' }}</span>
                </button>
              </div>
            </div>
          </section>

          <section class="kx-desktop-panel lead-table-panel">
            <div class="kx-desktop-panel-header">
              <div>
                <h2 class="kx-desktop-panel-title">{{ currentStatusTitle }}</h2>
                <span class="kx-desktop-panel-subtitle">{{ resultCountLabel }}</span>
              </div>
              <div class="lead-filter-labels">
                <button v-for="item in activeFilterLabels" :key="item.key" type="button" class="lead-stats-range" @click="removeFilter(item.key)">{{ item.label }}<van-icon aria-hidden="true" name="cross" /></button>
                <button v-if="activeFilterLabels.length > 1" type="button" class="lead-filter-reset-all" @click="clearAllFilters">清除全部</button>
              </div>
            </div>

            <div class="lead-table-scroll">
              <table class="kx-desktop-table lead-table">
                <colgroup>
				  <col v-if="canBatchOperate" class="lead-col-select" />
                  <col class="lead-col-customer" />
                  <col class="lead-col-progress" />
                  <col class="lead-col-source" />
                  <col class="lead-col-owner" />
                  <col class="lead-col-followup" />
                  <col class="lead-col-created" />
                </colgroup>
                <thead>
                  <tr>
					<th v-if="canBatchOperate" class="lead-select-column"><input type="checkbox" :checked="allVisibleSelected" aria-label="选择当前已加载线索" @change="toggleAllVisible" /></th>
                    <th>客户信息</th>
                    <th>当前进度</th>
                    <th>来源信息</th>
                    <th>负责人</th>
                    <th>下次回访</th>
                    <th>创建时间</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="lead in leads"
                    :key="lead.id"
                    class="lead-clickable-row"
                    :class="{ 'lead-row-overdue': followupSchedule(lead).overdue, 'lead-row-selected': selectedLeadIDs.has(lead.id) }"
                    tabindex="0"
                    @click="goToDetail(lead)"
                    @keydown.enter.self="goToDetail(lead)"
                  >
					<td v-if="canBatchOperate" class="lead-select-column" @click.stop><input type="checkbox" :checked="selectedLeadIDs.has(lead.id)" :aria-label="`选择线索 ${lead.lead_no}`" @change="toggleLeadSelection(lead)" /></td>
                    <td>
                      <div class="lead-customer-cell">
                        <span class="lead-title-link" :title="displayLeadTitle(lead)">
                          {{ displayLeadTitle(lead) }}
                        </span>
                        <span class="lead-customer-meta" :title="lead.lead_no || ''">
                          {{ customerSummary(lead) }}
                        </span>
                      </div>
                    </td>
                    <td>
                      <div class="lead-progress-cell">
                        <div class="lead-stage-status">
                          <StatusBadge :status="lead.status" :payment-status="lead.payment_status" />
                          <span class="lead-stage-pill" :class="isLeadFinished(lead) ? 'finished' : lead.business_stage || 'invitation'">{{ leadStageLabel(lead) }}</span>
                        </div>
                        <span v-if="leadNextStep(lead, userStore.userInfo?.wecom_userid)" class="lead-next-step">{{ leadNextStep(lead, userStore.userInfo?.wecom_userid) }}</span>
                        <div class="lead-star-row" :class="{ empty: !lead.star_rating }">
                          <CrmRate
                            v-if="lead.star_rating"
                            :model-value="lead.star_rating"
                            readonly
                            :size="12"
                            :gutter="1"
                            color="var(--kx-color-warning)"
                            void-color="var(--kx-color-border-strong)"
                          />
                          <span v-else>未评级</span>
                        </div>
                      </div>
                    </td>
                    <td>
                      <div class="lead-source-cell">
                        <span class="lead-source-text">{{ sourceInfo(lead).primary }}</span>
                        <span class="lead-secondary-text" :title="sourceInfo(lead).detail">{{ sourceInfo(lead).secondary }}</span>
                      </div>
                    </td>
                    <td>
                      <div class="lead-owner-cell">
                        <span class="lead-owner-primary">{{ ownerInfo(lead).primary }}</span>
                        <span v-if="ownerInfo(lead).secondary" class="lead-secondary-text">{{ ownerInfo(lead).secondary }}</span>
                      </div>
                    </td>
                    <td>
                      <div class="lead-followup-cell" :class="followupSchedule(lead).tone">
                        <span>{{ followupSchedule(lead).label }}</span>
                        <time v-if="followupSchedule(lead).exact">{{ followupSchedule(lead).exact }}</time>
                      </div>
                    </td>
                    <td>
                      <div class="lead-created-cell">
                        <div>
                          <span>{{ formatDate(lead.created_at) }}</span>
                          <time>{{ formatCompactDateTime(lead.created_at) }}</time>
                        </div>
                        <van-icon aria-hidden="true" name="arrow" class="lead-row-arrow" />
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

<CrmStatePanel v-if="loadError" state="error" :title="loadError" action-label="重试" @action="loadLeads(false)" />
            <van-loading v-else-if="loading && leads.length === 0" type="spinner" vertical class="list-state">
              加载中
            </van-loading>
            <van-empty v-else-if="!loading && leads.length === 0" :description="emptyLeadMessage">
              <button v-if="canCreateLead" type="button" class="kx-desktop-button kx-desktop-button-primary empty-lead-action" @click="goToCreate">
                新建线索
              </button>
            </van-empty>

            <div v-if="leads.length > 0" class="lead-table-footer">
              <span>已加载 {{ leads.length }} 条</span>
              <button
                v-if="hasMore"
                type="button"
                class="kx-desktop-button"
                :disabled="loading"
                @click="loadMore"
              >
                {{ loading ? '加载中' : '加载更多' }}
              </button>
              <span v-else>没有更多了</span>
            </div>
          </section>
          </section>
        </div>
      </div>
    </template>

    <template v-else>
      <header class="lead-mobile-heading"><div><h1>线索工作台</h1><p>{{ pageTitle }}</p></div><button type="button" aria-label="刷新线索" :disabled="loading" @click="refreshLeads"><van-icon aria-hidden="true" name="replay" /></button></header>

	  <section class="workbench-mobile">
        <div class="workbench-mobile-summary">
          <div v-for="item in workbenchSummaryItems.slice(0, 4)" :key="item.key">
            <span>{{ item.label }}</span>
            <strong :class="{ danger: item.danger }">{{ item.value }}</strong>
          </div>
        </div>
        <details v-if="workbench.tasks.length" class="workbench-mobile-tasks">
          <summary><span>异常待办</span><span>{{ workbench.tasks.length }} 项</span><van-icon aria-hidden="true" name="arrow-down" /></summary>
          <button
            v-for="task in workbench.tasks"
            :key="`${task.type}-${task.lead_id}-${task.appointment_id || 0}`"
            type="button"
            class="workbench-mobile-task"
            @click="openWorkbenchTask(task)"
          >
            <span>{{ workbenchTaskLabel(task) }}</span>
            <van-icon aria-hidden="true" name="arrow" />
          </button>
        </details>
        <button v-if="workbenchError" type="button" class="workbench-mobile-retry" @click="loadWorkbench">工作台加载失败，点击重试</button>
      </section>

      <section class="lead-mobile-controls" aria-label="线索筛选与视图">
      <div class="search-row lead-mobile-search-row">
        <van-search
          v-model="keyword"
          clearable
          placeholder="搜索姓名、手机号后四位、城市、来源"
          @search="onSearch"
          @clear="onSearch"
          @update:model-value="onKeywordInput"
        />
      </div>
      <div class="lead-mobile-status-row" role="group" aria-label="线索状态">
        <div class="lead-mobile-status-segment">
          <button v-for="option in mobilePrimaryStatuses" :key="option.name" type="button" :aria-label="option.title" :aria-pressed="activeTab === option.name" @click="setActiveStatus(option.name)">{{ option.mobileTitle || option.title }}</button>
        </div>
        <button v-if="statusOptions.length > mobilePrimaryStatuses.length" type="button" class="lead-mobile-status-more" :class="{ selected: mobileExtraStatus }" aria-label="选择线索状态" :aria-description="'当前：' + currentStatusTitle" aria-haspopup="dialog" :aria-expanded="showMobileMenu && mobileMenu === 'status'" @click="openMobileMenu('status')">更多<van-icon aria-hidden="true" name="arrow-down" /></button>
      </div>
      <div class="lead-mobile-tools">
        <button type="button" :class="{ active: activeFilterLabels.length }" :aria-label="`筛选线索，当前 ${activeFilterLabels.length} 个条件`" aria-haspopup="dialog" :aria-expanded="showMobileFilters" @click="showMobileFilters = true"><van-icon aria-hidden="true" name="filter-o" /><span>筛选</span><small v-if="activeFilterLabels.length">{{ activeFilterLabels.length }}</small></button>
        <button type="button" aria-label="线索排序" :aria-description="'当前：' + mobileSortTitle" aria-haspopup="dialog" :aria-expanded="showMobileMenu && mobileMenu === 'sort'" @click="openMobileMenu('sort')"><van-icon aria-hidden="true" name="sort" /><span>{{ mobileSortTitle }}</span></button>
        <button type="button" :class="{ active: selectedViewID }" aria-label="线索视图" :aria-description="'当前：' + mobileViewTitle" aria-haspopup="dialog" :aria-expanded="showMobileMenu && mobileMenu === 'views'" @click="openMobileMenu('views')"><van-icon aria-hidden="true" name="bookmark-o" /><span>视图</span><van-icon aria-hidden="true" name="arrow-down" /></button>
      </div>

      <div v-if="activeFilterLabels.length" class="lead-mobile-filter-labels"><button v-for="item in activeFilterLabels" :key="item.key" type="button" class="lead-stats-range" @click="removeFilter(item.key)">{{ item.label }}<van-icon aria-hidden="true" name="cross" /></button><button v-if="activeFilterLabels.length > 1" type="button" class="lead-filter-reset-all" @click="clearAllFilters">清除</button></div>
      <div class="lead-mobile-results"><span>{{ currentStatusTitle }} · {{ resultCountLabel }}</span><span v-if="selectedViewID" class="lead-mobile-current-view">{{ mobileViewTitle }}</span></div>
      </section>

      <section class="lead-mobile-surface" aria-label="线索列表">
      <van-pull-refresh v-model="refreshing" @refresh="onRefresh">
        <van-list
          v-model:loading="loading"
          :finished="!hasMore"
          :error="Boolean(loadError)"
          error-text=""
          finished-text="没有更多了"
          @load="onLoad"
        >
          <div class="lead-mobile-list">
            <div v-for="lead in leads" :key="lead.id" class="lead-mobile-select-row" :class="{ selected: selectedLeadIDs.has(lead.id), 'can-select': canBatchOperate }" role="article">
              <label v-if="canBatchOperate" class="lead-mobile-checkbox"><input type="checkbox" :checked="selectedLeadIDs.has(lead.id)" :aria-label="`选择线索 ${lead.lead_no}`" @change="toggleLeadSelection(lead)" /></label>
              <LeadCard :lead="lead" :actor-user-id="userStore.userInfo?.wecom_userid || ''" :show-dispatcher="userStore.isManager() || userStore.isAdmin() || userStore.isDispatcher()" @click="goToDetail" />
            </div>

            <CrmStatePanel v-if="loadError" state="error" :title="loadError" action-label="重试" @action="loadLeads(false)" />
            <van-empty v-else-if="!loading && leads.length === 0" :description="emptyLeadMessage">
              <van-button v-if="canCreateLead" size="small" type="primary" @click="goToCreate">新建线索</van-button>
            </van-empty>
          </div>
        </van-list>
      </van-pull-refresh>
      </section>

      <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="mobileMenuPopup" v-model:show="showMobileMenu" :aria-label="mobileMenuTitle" aria-modal="true" position="bottom" round class="lead-mobile-menu">
        <div class="lead-mobile-menu-heading"><h2>{{ mobileMenuTitle }}</h2><button type="button" data-autofocus :aria-label="'关闭' + mobileMenuTitle" @click="showMobileMenu = false"><van-icon aria-hidden="true" name="cross" /></button></div>
        <div v-if="mobileMenu === 'status'" class="lead-mobile-menu-options" role="group" aria-label="全部线索状态">
          <button v-for="option in statusOptions" :key="option.name" type="button" :aria-pressed="activeTab === option.name" @click="selectMobileStatus(option.name)"><span>{{ option.title }}</span><van-icon v-if="activeTab === option.name" aria-hidden="true" name="success" /></button>
        </div>
        <div v-else-if="mobileMenu === 'sort'" class="lead-mobile-menu-options" role="group" aria-label="线索排序方式">
          <button v-for="option in mobileSortOptions" :key="option.value" type="button" :aria-pressed="sortValue === option.value" @click="selectMobileSort(option.value)"><span>{{ option.title }}</span><van-icon v-if="sortValue === option.value" aria-hidden="true" name="success" /></button>
        </div>
        <template v-else>
          <p class="lead-mobile-menu-note">已保存 {{ savedViews.length }} / 20 个视图</p>
          <div class="lead-mobile-menu-options" role="group" aria-label="保存的线索视图">
            <button type="button" :aria-pressed="!selectedViewID" @click="selectMobileView('')"><span>当前视图</span><van-icon v-if="!selectedViewID" aria-hidden="true" name="success" /></button>
            <button v-for="view in savedViews" :key="view.id" type="button" :aria-pressed="selectedViewID === String(view.id)" @click="selectMobileView(String(view.id))"><span>{{ view.name }}</span><van-icon v-if="selectedViewID === String(view.id)" aria-hidden="true" name="success" /></button>
          </div>
          <div class="lead-mobile-menu-options lead-mobile-view-options">
            <button type="button" :disabled="savedViews.length >= 20" @click="openMobileViewEditor(false)"><span>保存当前视图</span><van-icon aria-hidden="true" name="plus" /></button>
            <button v-if="selectedViewID" type="button" @click="openMobileViewEditor(true)"><span>管理当前视图</span><van-icon aria-hidden="true" name="arrow" /></button>
          </div>
          <p v-if="savedViews.length >= 20" class="lead-mobile-menu-note" role="status">已达 20 个上限，选择已有视图后可在管理视图中更新或删除。</p>
        </template>
      </van-popup>

      <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="showMobileFiltersPopup" aria-label="筛选线索" aria-modal="true" v-model:show="showMobileFilters" position="bottom" round class="lead-filter-popup">
        <div class="lead-filter-popup-heading"><div><strong>筛选线索</strong><span>按负责人和创建日期缩小范围</span></div><button type="button" aria-label="关闭筛选" @click="showMobileFilters = false"><van-icon aria-hidden="true" name="cross" /></button></div>
        <label v-if="showInviterFilter" class="lead-filter-field"><span>负责人</span><select v-model="salesUserID"><option value="">全部人员</option><option v-for="item in salesUsers" :key="item.wecom_userid" :value="item.wecom_userid">{{ item.role === 'manager' ? '主管 · ' : '' }}{{ item.employee_name || item.wecom_userid }}</option></select></label>
        <div class="lead-filter-date-fields"><label><span>开始日期</span><input v-model="startDate" type="date" /></label><label><span>结束日期</span><input v-model="endDate" type="date" /></label></div>
        <div class="lead-filter-popup-actions"><button type="button" @click="clearAllFilters">重置</button><button type="button" class="primary" @click="applyMobileFilters">应用筛选</button></div>
      </van-popup>
	</template>
	  <van-popup :overlay-props="{ 'aria-hidden': true, tabindex: -1, role: 'presentation' }" ref="showSaveViewPopup" :aria-label="editingView ? '管理视图' : '保存视图'" aria-modal="true" v-model:show="showSaveView" :position="mobileLayout ? 'bottom' : 'center'" round class="lead-filter-popup">
      <div class="lead-filter-popup-heading"><div><strong>{{ editingView ? '管理已保存视图' : '保存当前视图' }}</strong><span>最多 20 个；更新条件使用当前筛选与排序</span></div><button type="button" aria-label="关闭" @click="showSaveView = false"><van-icon aria-hidden="true" name="cross" /></button></div>
      <label class="lead-filter-field"><span>视图名称</span><input ref="saveViewInput" v-model="newViewName" maxlength="64" placeholder="例如：本周待跟进" /></label>
      <p v-if="viewError" ref="viewErrorSummary" role="alert" tabindex="-1">{{ viewError }} <button type="button" @click="refreshViewVersion">刷新视图版本（保留名称）</button></p>
      <div class="lead-filter-popup-actions"><button type="button" @click="showSaveView = false">取消</button><button type="button" class="primary" :disabled="savingView" @click="saveCurrentView">{{ editingView ? '重命名' : '保存' }}</button></div>
      <div v-if="editingView" class="lead-filter-popup-actions"><button type="button" :disabled="savingView" @click="saveCurrentView(true)">更新为当前筛选条件</button><button type="button" :disabled="savingView" @click="removeCurrentView">删除视图</button></div>
    </van-popup>

	  <div v-if="selectedLeadIDs.size" class="lead-batch-bar" role="region" aria-label="批量操作"><strong>已选 {{ selectedLeadIDs.size }} 条</strong><span>选择只保留在当前页</span><select v-model="batchTargetUserID" aria-label="批量操作目标人员"><option value="">选择目标人员</option><option v-for="item in batchTargets" :key="item.wecom_userid" :value="item.wecom_userid">{{ item.role === 'manager' ? '主管 · ' : '' }}{{ item.employee_name || item.wecom_userid }}</option></select><button type="button" @click="clearSelection">取消</button><button type="button" class="primary" :disabled="batchSubmitting || !batchTargetUserID" @click="submitBatchOperation">{{ batchSubmitting ? '处理中' : batchActionLabel }}</button></div>
  </div>
</template>

<script setup>
import CrmRate from '@/components/CrmRate.vue'
import { useOverlayFocus } from '@/composables/useOverlayFocus'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showConfirmDialog, showFailToast, showSuccessToast } from 'vant'
import { batchAssignLeads, batchReassignLeads, getLeadList } from '@/api/lead'
import { createLeadView, updateLeadView, deleteLeadView, getLeadListPreferences, getLeadViews } from '@/api/leadPreferences'
import { getCloserUsers, getLeadOwnerFilters, getSalesUsers } from '@/api/user'
import { getWorkbench } from '@/api/workbench'
import { useUserStore } from '@/stores/user'
import CrmStatePanel from '@/components/CrmStatePanel.vue'
import LeadCard from '@/components/LeadCard.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useResponsiveLayout } from '@/composables/useResponsiveLayout'
import { describeFollowupSchedule, formatCompactDateTime, formatDate } from '@/utils/date'
import {
  displayCustomerName,
  displayLeadDispatcher,
  displayLeadOwner,
  displayLeadTitle,
  displaySourceCampaign,
  displaySourceCampaignSummary,
  displaySourceChannel, leadStageLabel, isLeadFinished
} from '@/utils/leadDisplay'
import { displayLeadMobile } from '@/utils/leadContact'
import { leadNextStep, preferredLeadView, rememberLeadView, workbenchTaskLabel } from '@/utils/leadTriage'
import { queryFingerprint } from '@/utils/leadListQuery'

const router = useRouter()
const route = useRoute()
const userStore = useUserStore()
const { isMobile: mobileLayout } = useResponsiveLayout()
const canCreateLead = computed(() => userStore.canCreateLead())
const pageTitle = computed(() => {
  if (userStore.isDispatcher()) return '线索分配工作台'
  if (userStore.isManager()) return '邀约与谈单工作台'
  return userStore.isCloser() ? '我的谈单客户' : '我的邀约线索'
})

const defaultActiveTab = () => {
  if (userStore.isDispatcher()) return 'dispatcher_pending'
  if (userStore.isCloser()) return 'following'
  return preferredLeadView(userStore.userInfo?.wecom_userid)
}
const queryValue = value => Array.isArray(value) ? value[0] : (typeof value === 'string' ? value : '')
const supportedStatuses = new Set(['needs_action', 'pending_dispatch', 'assigned_pending_claim', 'following', 'followup_overdue', 'converted', 'invalid', 'all', 'dispatcher_pending', 'dispatcher_handled'])
const requestedStatus = queryValue(route.query.status)
const dispatcherStatuses = new Set(['dispatcher_pending', 'dispatcher_handled'])
const requestedStatusSupported = userStore.isDispatcher()
  ? dispatcherStatuses.has(requestedStatus)
  : supportedStatuses.has(requestedStatus) && !dispatcherStatuses.has(requestedStatus)
const activeTab = ref(requestedStatusSupported ? requestedStatus : defaultActiveTab())
const startDate = ref(queryValue(route.query.start_date))
const endDate = ref(queryValue(route.query.end_date))
const salesUserID = ref((userStore.isManager() || userStore.isAdmin()) ? queryValue(route.query.owner || route.query.sales_userid) : '')
const salesUsers = ref([])
const showDateFilter = ref(false)
const showMobileFilters = ref(false)
const showMobileFiltersPopup = ref(null)
useOverlayFocus(showMobileFilters, showMobileFiltersPopup, () => { showMobileFilters.value = false })
const showMobileMenu = ref(false)
const mobileMenu = ref('status')
const mobileMenuPopup = ref(null)
useOverlayFocus(showMobileMenu, mobileMenuPopup, () => { showMobileMenu.value = false })
const mobileMenuTitle = computed(() => ({ status: '选择线索状态', sort: '排序方式', views: '线索视图' })[mobileMenu.value])
const leads = ref([])
const loading = ref(false)
const loadError = ref('')
const refreshing = ref(false)
const hasMore = ref(true)
const requestSeq = ref(0)
const keyword = ref(queryValue(route.query.keyword))
const sortBy = ref(queryValue(route.query.sort_by) || 'updated_at')
const sortOrder = ref(queryValue(route.query.sort_order) || 'desc')
const sortValue = computed({ get: () => `${sortBy.value}:${sortOrder.value}`, set: value => { [sortBy.value, sortOrder.value] = value.split(':') } })
const savedViews = ref([])
const selectedViewID = ref('')
const showSaveView = ref(false)
const showSaveViewPopup = ref(null)
useOverlayFocus(showSaveView, showSaveViewPopup, () => { showSaveView.value = false })
const newViewName = ref('')
const editingView = ref(null)
const viewError = ref('')
const viewErrorSummary = ref(null)
const savingView = ref(false)
const selectedLeadIDs = ref(new Set())
const batchTargetUserID = ref('')
const batchSubmitting = ref(false)
const closerUsers = ref([])
const totalCount = ref(0)
const workbench = ref({ summary: {}, tasks: [] })
const workbenchLoading = ref(false)
const workbenchError = ref(false)
let searchTimer = null

const pagination = reactive({
  page: 1,
  pageSize: 20
})

const statusOptions = computed(() => {
  if (userStore.isDispatcher()) {
    return [
      { title: '待分配', name: 'dispatcher_pending' },
      { title: '我的经手', name: 'dispatcher_handled' }
    ]
  }
  const options = []
  if (!userStore.isCloser()) options.push({ title: userStore.isManager() ? '团队待处理' : '待我处理', name: 'needs_action' })
  if (userStore.isManager() || userStore.isAdmin()) {
    options.push({ title: '待分配邀约', name: 'pending_dispatch' })
  }
  options.push({ title: userStore.isCloser() ? '待邀约师领取' : '待领取', name: 'assigned_pending_claim' })
  options.push(
    { title: '跟进中', name: 'following' },
    { title: '回访逾期', name: 'followup_overdue' },
    { title: '已成交', name: 'converted' },
    { title: '无效线索', name: 'invalid' },
    { title: '全部', name: 'all' }
  )
  return options
})

const currentStatusTitle = computed(() => {
  return statusOptions.value.find(option => option.name === activeTab.value)?.title || '全部'
})
const mobilePrimaryStatuses = computed(() => {
  if (userStore.isDispatcher()) return statusOptions.value
  const names = userStore.isCloser()
    ? ['all', 'following', 'followup_overdue']
    : ['needs_action', 'following', 'all']
  return names.map(name => statusOptions.value.find(option => option.name === name)).filter(Boolean)
    .map(option => ({ ...option, mobileTitle: option.name === 'pending_dispatch' ? '待分配' : option.title }))
})
const mobileExtraStatus = computed(() => !mobilePrimaryStatuses.value.some(option => option.name === activeTab.value))
const mobileSortOptions = [
  { value: 'updated_at:desc', title: '最近更新' },
  { value: 'created_at:desc', title: '最新创建' },
  { value: 'created_at:asc', title: '最早创建' },
  { value: 'next_followup_due_at:asc', title: '回访时间优先' },
  { value: 'assigned_at:desc', title: '最近分配' },
  { value: 'converted_at:desc', title: '最近成交' }
]
const mobileSortTitle = computed(() => mobileSortOptions.find(option => option.value === sortValue.value)?.title || '排序')
const mobileViewTitle = computed(() => savedViews.value.find(view => String(view.id) === selectedViewID.value)?.name || '当前视图')
const openMobileMenu = menu => { mobileMenu.value = menu; showMobileMenu.value = true }
const selectMobileStatus = status => { showMobileMenu.value = false; setActiveStatus(status) }
const selectMobileSort = value => { showMobileMenu.value = false; sortValue.value = value; onSortChange() }
const selectMobileView = id => { showMobileMenu.value = false; selectedViewID.value = id; applySelectedView() }
const openMobileViewEditor = async edit => {
  showMobileMenu.value = false
  // Return focus to the toolbar before opening the editor above the same page.
  await nextTick()
  openViewEditor(edit)
}
const showInviterFilter = computed(() => userStore.isManager() || userStore.isAdmin())
const showAllPeopleSelect = computed(() => (userStore.isManager() || userStore.isAdmin()) && activeTab.value === 'all')
const dateFilterLabel = computed(() => startDate.value && endDate.value ? `${startDate.value.slice(5)} 至 ${endDate.value.slice(5)}` : '日期筛选')
const selectedOwnerName = computed(() => salesUsers.value.find(item => item.wecom_userid === salesUserID.value)?.employee_name || salesUserID.value)
const activeFilterLabels = computed(() => [
  keyword.value.trim() ? { key: 'keyword', label: `关键词：${keyword.value.trim()}` } : null,
  salesUserID.value ? { key: 'owner', label: `负责人：${selectedOwnerName.value}` } : null,
  startDate.value && endDate.value ? { key: 'date', label: startDate.value === endDate.value ? startDate.value : `${startDate.value} 至 ${endDate.value}` } : null
].filter(Boolean))

const canBatchOperate = computed(() => userStore.isAdmin() || userStore.isManager() || userStore.isDispatcher())
const selectedLeads = computed(() => leads.value.filter(item => selectedLeadIDs.value.has(item.id)))
const allVisibleSelected = computed(() => leads.value.length > 0 && leads.value.every(item => selectedLeadIDs.value.has(item.id)))
const selectedBatchStage = computed(() => {
  const stages = new Set(selectedLeads.value.map(item => item.business_stage || 'invitation'))
  return stages.size === 1 ? [...stages][0] : ''
})
const batchIsInitialAssign = computed(() => selectedLeads.value.length > 0 && selectedLeads.value.every(item => item.status === 'pending_dispatch'))
const batchActionLabel = computed(() => batchIsInitialAssign.value ? '批量分配' : '批量转派')
const batchTargets = computed(() => selectedBatchStage.value === 'negotiation' ? closerUsers.value : salesUsers.value)

const currentListQuery = () => {
  const query = { ...route.query, status: activeTab.value }
  delete query.sales_userid
  if (salesUserID.value) query.owner = salesUserID.value
  else delete query.owner
  if (keyword.value.trim()) query.keyword = keyword.value.trim()
  else delete query.keyword
  if (startDate.value && endDate.value) {
    query.start_date = startDate.value
    query.end_date = endDate.value
  } else {
    delete query.start_date
    delete query.end_date
  }
	query.sort_by = sortBy.value
	query.sort_order = sortOrder.value
  return query
}

const replaceListQuery = () => router.replace({ path: '/leads', query: currentListQuery() })

const onSortChange = () => { replaceListQuery(); clearSelection(); loadLeads(true) }

const currentViewFilters = () => ({
  status: activeTab.value,
  keyword: keyword.value.trim(),
  sales_userid: salesUserID.value,
  dispatcher_scope: userStore.isDispatcher() ? (activeTab.value === 'dispatcher_handled' ? 'handled' : 'pending') : '',
  start_date: startDate.value,
  end_date: endDate.value
})

const loadSavedViews = async () => {
  try { const [preferences, views] = await Promise.all([getLeadListPreferences(), getLeadViews()]); savedViews.value = views.items || []; if (!Object.keys(route.query).length && preferences.default_view_id) { selectedViewID.value = String(preferences.default_view_id); applySelectedView() } } catch (error) { console.error('加载线索视图失败:', error) }
}

const applySelectedView = () => {
  const view = savedViews.value.find(item => String(item.id) === selectedViewID.value)
  if (!view) return
  const filters = view.filters || {}
  activeTab.value = filters.status || defaultActiveTab(); keyword.value = filters.keyword || ''; salesUserID.value = filters.sales_userid || ''; startDate.value = filters.start_date || ''; endDate.value = filters.end_date || ''
  sortBy.value = view.sort_by || 'updated_at'; sortOrder.value = view.sort_order || 'desc'; replaceListQuery(); clearSelection(); loadLeads(true)
}

const openViewEditor = edit => {
  editingView.value = edit ? savedViews.value.find(item => String(item.id) === selectedViewID.value) : null
  newViewName.value = editingView.value?.name || ''
  viewError.value = ''
  showSaveView.value = true
}
const refreshViewVersion = async () => {
  try { const data = await getLeadViews(); savedViews.value = data.items || []; editingView.value = savedViews.value.find(item => item.id === editingView.value?.id) || null; viewError.value = editingView.value ? '已刷新版本，请核对后重试' : '该视图已被删除，名称已保留，可另存新视图' } catch { viewError.value = '刷新失败，请重试' }
}
const saveCurrentView = async (replaceFilters = false) => {
  if (savingView.value) return
  const name = newViewName.value.trim(); if (!name) { showFailToast('请输入视图名称'); return }
  if (!editingView.value && savedViews.value.length >= 20) { viewError.value = '已达 20 个上限，请先删除不再使用的视图'; return }
  savingView.value = true; viewError.value = ''
  try {
    const current = editingView.value
    const payload = { name, filters: current && replaceFilters !== true ? current.filters : currentViewFilters(), sort_by: current && replaceFilters !== true ? current.sort_by : sortBy.value, sort_order: current && replaceFilters !== true ? current.sort_order : sortOrder.value, version: current?.version }
    const saved = current ? await updateLeadView(current.id, payload) : await createLeadView(payload)
    savedViews.value = [saved, ...savedViews.value.filter(item => item.id !== saved.id)]; selectedViewID.value = String(saved.id); showSaveView.value = false; showSuccessToast('视图已保存')
  } catch (error) { viewError.value = error?.response?.data?.error === 'lead_view_limit' ? '已达 20 个上限，请管理已有视图' : error?.response?.status === 409 ? '视图版本或名称冲突，输入已保留，请刷新或更换名称' : '保存失败，输入已保留，请重试' } finally {
    savingView.value = false
    if (viewError.value && showSaveView.value) {
      await nextTick()
      viewErrorSummary.value?.focus()
    }
  }
}
const removeCurrentView = async () => {
  if (!editingView.value || savingView.value) return
  try { await showConfirmDialog({ title: '删除视图', message: '仅删除保存的筛选条件，客户线索不受影响。' }) } catch { return }
  savingView.value = true
  try { const id = editingView.value.id; await deleteLeadView(id); savedViews.value = savedViews.value.filter(item => item.id !== id); selectedViewID.value = ''; showSaveView.value = false; showSuccessToast('视图已删除') } catch { viewError.value = '删除失败，请重试' } finally { savingView.value = false }
}

const clearSelection = () => { selectedLeadIDs.value = new Set(); batchTargetUserID.value = '' }
const toggleLeadSelection = async lead => {
  const next = new Set(selectedLeadIDs.value); if (next.has(lead.id)) next.delete(lead.id); else next.add(lead.id); selectedLeadIDs.value = next; batchTargetUserID.value = ''
  if (next.size && (lead.business_stage || 'invitation') === 'negotiation' && closerUsers.value.length === 0) { try { const data = await getCloserUsers(); closerUsers.value = data.bindings || [] } catch { showFailToast('谈单师列表加载失败') } }
  if (next.size && salesUsers.value.length === 0) { try { await loadSalesUsers() } catch { showFailToast('负责人列表加载失败') } }
}
const toggleAllVisible = async event => {
  selectedLeadIDs.value = event.target.checked ? new Set(leads.value.map(item => item.id)) : new Set()
  batchTargetUserID.value = ''
  if (!selectedLeadIDs.value.size) return
  try {
    if (selectedBatchStage.value === 'negotiation' && !closerUsers.value.length) {
      const data = await getCloserUsers()
      closerUsers.value = data.bindings || []
    } else await loadSalesUsers()
  } catch { showFailToast('负责人列表加载失败，请重新选择后重试') }
}

const submitBatchOperation = async () => {
  if (batchSubmitting.value || !selectedLeads.value.length || !batchTargetUserID.value) return
  if (!batchIsInitialAssign.value && userStore.isDispatcher()) { showFailToast('线索调度员只能执行首次分配'); return }
  if (!batchIsInitialAssign.value && !selectedBatchStage.value) { showFailToast('批量转派必须选择同一业务阶段的线索'); return }
  const invalid = batchIsInitialAssign.value ? selectedLeads.value.some(item => item.status !== 'pending_dispatch') : false
  if (invalid) { showFailToast('首次分配只能选择待分配线索'); return }
  try { await showConfirmDialog({ title: batchActionLabel.value, message: `确认将 ${selectedLeads.value.length} 条线索${batchIsInitialAssign.value ? '分配' : '转派'}给所选人员？整批将同时成功或回滚。` }) } catch { return }
  batchSubmitting.value = true
  const payload = { items: selectedLeads.value.map(item => ({ lead_id: item.id, version: item.version, profile_version: item.profile_version })), target_user_id: batchTargetUserID.value, business_stage: selectedBatchStage.value || 'invitation', remark: `CRM 批量${batchIsInitialAssign.value ? '分配' : '转派'}` }
  try { if (batchIsInitialAssign.value) await batchAssignLeads(payload); else await batchReassignLeads(payload); showSuccessToast(`已处理 ${selectedLeads.value.length} 条线索`); clearSelection(); await Promise.all([loadLeads(true), loadWorkbench()]) } catch (error) { const numbers = error?.response?.data?.lead_nos; showFailToast(numbers?.length ? `以下线索已变化：${numbers.slice(0, 3).join('、')}` : '批量操作失败，请刷新后重试') } finally { batchSubmitting.value = false }
}

const loadSalesUsers = async () => {
  if (salesUsers.value.length || !canBatchOperate.value) return
  const data = await (userStore.isDispatcher() ? getSalesUsers() : getLeadOwnerFilters())
  salesUsers.value = data.bindings || []
}

const onSalesFilterChange = () => {
  replaceListQuery()
  loadLeads(true)
}

const applyDateFilter = () => {
  if (!startDate.value || !endDate.value) { showFailToast('请选择完整的开始和结束日期'); return }
  if (startDate.value > endDate.value) { showFailToast('结束日期不能早于开始日期'); return }
  replaceListQuery()
  showDateFilter.value = false
  loadLeads(true)
}

const clearDateFilter = () => {
  startDate.value = ''; endDate.value = ''
  replaceListQuery()
  showDateFilter.value = false
  loadLeads(true)
}

const resultCountLabel = computed(() => {
  if (loading.value && leads.value.length === 0) {
    return '正在加载'
  }
  if (totalCount.value > 0) {
    return `共 ${totalCount.value} 条，已加载 ${leads.value.length} 条`
  }
  return leads.value.length > 0 ? `已加载 ${leads.value.length} 条` : '暂无数据'
})

const emptyLeadMessage = computed(() => {
  if (keyword.value.trim()) return '没有匹配的线索，请尝试其他关键词'
  if (statsRangeLabel.value) return '该统计范围内暂无线索'
  if (activeTab.value === 'needs_action') return '当前没有待分配、待领取或逾期回访线索'
  if (activeTab.value === 'assigned_pending_claim') return '暂无待领取线索'
  if (['pending_dispatch', 'dispatcher_pending'].includes(activeTab.value)) return '暂无待分配线索'
  if (activeTab.value === 'dispatcher_handled') return '暂无由你创建或分配过的线索'
  return '当前状态下暂无线索'
})

const statsRangeLabel = computed(() => {
  if (!startDate.value || !endDate.value) return ''
  const dateLabel = startDate.value === endDate.value
    ? startDate.value
    : `${startDate.value} 至 ${endDate.value}`
  return `按线索录入日期：${dateLabel}${salesUserID.value ? ' · 指定负责人' : ''}`
})

const workbenchSummaryItems = computed(() => {
  const summary = workbench.value.summary || {}
	const items = userStore.isDispatcher()
	  ? [['pending_dispatch', '待分配'], ['handled_today', '今日经手']]
	  : userStore.isManager() || userStore.isAdmin()
    ? [
        ['pending_dispatch', '待分配线索'],
        ['pending_claim', '待领取线索'],
        ['appointment_pending_assignment', '待分配预约'],
        ['overdue_followup', '逾期回访']
      ]
    : userStore.isCloser()
      ? [
          ['pending_claim', '待邀约师领取'],
          ['today_appointments', '今日预约'],
          ['overdue_followup', '逾期回访'],
          ['negotiation_active', '谈单跟进中']
        ]
      : [
          ['pending_claim', '待领取'],
          ['overdue_followup', '逾期回访'],
          ['appointment_unconfirmed', '到时未确认'],
          ['today_appointments', '今日预约']
        ]
  return items.map(([key, label]) => ({ key, label, value: Number(summary[key] || 0), danger: ['overdue_followup', 'appointment_unconfirmed'].includes(key) && Number(summary[key] || 0) > 0 }))
})

const uniqueTextParts = parts => [...new Set(parts.map(value => String(value || '').trim()).filter(Boolean))]

const customerSummary = lead => {
  const title = displayLeadTitle(lead)
  const name = displayCustomerName(lead)
  const mobile = displayLeadMobile(lead, userStore.isDispatcher())
  return uniqueTextParts([name !== title ? name : '', mobile]).join(' · ')
}

const sourceInfo = lead => {
  const source = displaySourceChannel(lead.source_channel)
  const [primary, ...accountParts] = source === '-' ? [] : source.split(' · ')
  const account = accountParts.join(' · ')
  const secondary = uniqueTextParts([
    account,
    displaySourceCampaignSummary(lead.source_channel, lead.source_campaign),
    lead.city
  ]).join(' · ')
  const detail = uniqueTextParts([
    source === '-' ? '' : source,
    displaySourceCampaign(lead.source_campaign),
    lead.city
  ]).join(' · ')
  return {
    primary: primary || '来源未填写',
    secondary: secondary || '暂无补充信息',
    detail: detail || '暂无补充信息'
  }
}

const ownerInfo = lead => {
  const owner = displayLeadOwner(lead)
  const roleName = lead.business_stage === 'negotiation' ? '谈单师' : '邀约师'
  let primary = owner ? `${roleName}：${owner}` : '负责人待确认'
  if (!owner && lead.status === 'pending_dispatch') primary = '待分配邀约师'
  if (!owner && lead.status === 'assigned_pending_claim') primary = '待邀约师领取'

  const dispatcher = userStore.isManager() || userStore.isAdmin() || userStore.isDispatcher()
    ? displayLeadDispatcher(lead)
    : ''
  return {
    primary,
    secondary: dispatcher ? `分配人：${dispatcher}` : ''
  }
}

const followupSchedule = lead => isLeadFinished(lead) ? { label: '已结束', tone: 'normal', overdue: false } : describeFollowupSchedule(lead.next_followup_due_at)

const loadWorkbench = async () => {
  workbenchLoading.value = true
  workbenchError.value = false
  try {
    const data = await getWorkbench()
    workbench.value = { summary: data?.summary || {}, tasks: data?.tasks || [] }
  } catch (error) {
    console.error('获取工作台失败:', error)
    workbenchError.value = true
  } finally {
    workbenchLoading.value = false
  }
}

const openWorkbenchTask = (task) => {
  if (task.appointment_id) {
    router.push('/appointments')
    return
  }
  goToDetail({ id: task.lead_id })
}

const uniqueById = (items) => {
  const seen = new Set()
  return items.filter(item => {
    if (seen.has(item.id)) {
      return false
    }
    seen.add(item.id)
    return true
  })
}

const loadLeads = async (reset = false, options = {}) => {
  if (loading.value && !reset && !options.ignoreLoading) {
    return
  }
  loadError.value = ''
  const requestId = ++requestSeq.value
  if (reset) {
    clearSelection()
    pagination.page = 1
    leads.value = []
    hasMore.value = true
    totalCount.value = 0
  }
  loading.value = true

  try {
    const params = {
      page: pagination.page,
      page_size: pagination.pageSize
    }
    
    if (userStore.isDispatcher()) {
      params.dispatcher_scope = activeTab.value === 'dispatcher_handled' ? 'handled' : 'pending'
    } else if (activeTab.value !== 'all') {
      params.status = activeTab.value
    }
    const searchKeyword = keyword.value.trim()
    if (searchKeyword) {
      params.keyword = searchKeyword
    }
    if (startDate.value && endDate.value) {
      params.start_date = startDate.value
      params.end_date = endDate.value
    }
    if (salesUserID.value) {
      params.sales_userid = salesUserID.value
    }
	params.sort_by = sortBy.value
	params.sort_order = sortOrder.value

    const data = await getLeadList(params)
    if (requestId !== requestSeq.value) {
      return
    }

    const items = uniqueById(data.leads || data.Leads || data.items || [])
    const currentPage = Number(data.page || data.Page || pagination.page)
    const pageSize = Number(data.page_size || data.PageSize || pagination.pageSize)
    
    if (reset) {
      leads.value = items
    } else {
      const existingIds = new Set(leads.value.map(item => item.id))
      leads.value.push(...items.filter(item => !existingIds.has(item.id)))
    }

    const total = data.total ?? data.Total
    totalCount.value = total === undefined || total === null ? leads.value.length : Number(total)

    const totalPages = data.total_pages || data.TotalPages
    hasMore.value = totalPages ? currentPage < Number(totalPages) : items.length >= pageSize
    
    pagination.page = currentPage + 1
  } catch {
    if (requestId === requestSeq.value) loadError.value = '线索加载失败，筛选条件已保留，请重试'
  } finally {
    if (requestId === requestSeq.value) {
      loading.value = false
    }
  }
}

const onRefresh = async () => {
	const tasks = [loadLeads(true), loadWorkbench()]
  await Promise.all(tasks)
  refreshing.value = false
}

const onLoad = async () => {
  await loadLeads(false, { ignoreLoading: true })
}

const onSearch = () => {
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
  replaceListQuery()
  loadLeads(true).finally(() => {
    refreshing.value = false
  })
}

const refreshLeads = () => {
	const tasks = [loadLeads(true), loadWorkbench()]
  Promise.all(tasks).finally(() => {
    refreshing.value = false
  })
}

const onKeywordInput = () => {
  if (searchTimer) {
    clearTimeout(searchTimer)
  }
  searchTimer = setTimeout(() => {
    onSearch()
  }, 350)
}

const onTabChange = () => {
  rememberLeadView(userStore.userInfo?.wecom_userid, activeTab.value)
	if (activeTab.value === 'all') loadSalesUsers()
  replaceListQuery()
  loadLeads(true).finally(() => {
    refreshing.value = false
  })
}

const setActiveStatus = (status) => {
  if (activeTab.value === status) {
    return
  }
  activeTab.value = status
  onTabChange()
}

const removeFilter = key => {
  if (key === 'keyword') keyword.value = ''
  if (key === 'owner') salesUserID.value = ''
  if (key === 'date') { startDate.value = ''; endDate.value = '' }
  replaceListQuery()
  loadLeads(true)
}

const clearAllFilters = () => {
  keyword.value = ''
  salesUserID.value = ''
  startDate.value = ''
  endDate.value = ''
  replaceListQuery()
  showMobileFilters.value = false
  loadLeads(true)
}

const applyMobileFilters = () => {
  if ((startDate.value || endDate.value) && (!startDate.value || !endDate.value)) {
    showFailToast('请选择完整的开始和结束日期')
    return
  }
  if (startDate.value && startDate.value > endDate.value) {
    showFailToast('结束日期不能早于开始日期')
    return
  }
  replaceListQuery()
  showMobileFilters.value = false
  loadLeads(true)
}

const loadMore = () => {
  if (!hasMore.value) {
    return
  }
	clearSelection()
  loadLeads(false)
}

const goToDetail = async (lead) => {
  sessionStorage.setItem('kxone.leads.scrollY', String(window.scrollY || 0))
  if (searchTimer) { clearTimeout(searchTimer); searchTimer = null }
  await replaceListQuery()
  router.push({ path: `/leads/${lead.id}`, query: { return_to: route.fullPath } })
}

const goToCreate = () => {
  router.push('/leads/new')
}

onBeforeUnmount(() => {
  if (searchTimer) {
    clearTimeout(searchTimer)
  }
})

watch(() => route.fullPath, () => {
  if (route.path !== '/leads' || queryFingerprint(route.query) === queryFingerprint(currentListQuery())) return
  if (searchTimer) { clearTimeout(searchTimer); searchTimer = null }
  const status = queryValue(route.query.status)
  const valid = userStore.isDispatcher() ? dispatcherStatuses.has(status) : supportedStatuses.has(status) && !dispatcherStatuses.has(status) && !(userStore.isCloser() && status === 'needs_action')
  activeTab.value = valid ? status : defaultActiveTab()
  startDate.value = queryValue(route.query.start_date)
  endDate.value = queryValue(route.query.end_date)
  salesUserID.value = showInviterFilter.value ? queryValue(route.query.owner || route.query.sales_userid) : ''
  keyword.value = queryValue(route.query.keyword)
  sortBy.value = queryValue(route.query.sort_by) || 'updated_at'
  sortOrder.value = queryValue(route.query.sort_order) || 'desc'
  selectedViewID.value = ''
  loadLeads(true)
})

onMounted(() => {
	loadSavedViews()
	loadWorkbench()
	if (showInviterFilter.value) loadSalesUsers()
	if (mobileLayout.value) {
    return
  }
  loadLeads(true).finally(() => {
    refreshing.value = false
    const scrollY = Number(sessionStorage.getItem('kxone.leads.scrollY') || 0)
    if (scrollY > 0) requestAnimationFrame(() => window.scrollTo({ top: scrollY }))
  })
})
</script>

<style scoped>
.lead-next-step { display: block; color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); line-height: 1.5; overflow-wrap: anywhere; }
.lead-stage-pill.finished { color: var(--kx-color-text-muted); }
.lead-stage-status > * { flex: 0 1 auto; max-width: 100%; }
.page-container.has-batch-selection { padding-bottom: 100px; }
.page-container {
  background-color: var(--kx-color-canvas);
  min-height: 100vh;
}

:deep(.van-nav-bar__right) {
  gap: 16px;
}

.lead-desktop-page { padding: var(--kx-space-6); background: var(--kx-color-canvas); }

.lead-desktop-container {
  display: flex;
  flex-direction: column;
  min-height: calc(100vh - 48px);
}

.lead-list-surface { margin-top: var(--kx-space-6); border: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-subtle); }
.lead-desktop-page .kx-desktop-title { font-size: var(--kx-font-26); }
.lead-desktop-page .kx-desktop-subtitle { margin-top: var(--kx-space-2); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); }
.lead-toolbar { padding: var(--kx-space-4); margin: 0; align-items: stretch; flex-direction: column; border: 0; border-bottom: 1px solid var(--kx-color-border); border-radius: var(--kx-card-radius) var(--kx-card-radius) 0 0; background: var(--kx-color-surface); box-shadow: none; }

.lead-toolbar-select, .lead-view-save { height: var(--kx-control-height); border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); font-size: 13px; white-space: nowrap; }
.lead-toolbar-select { max-width: 138px; padding: 0 28px 0 10px; }
.lead-view-save { padding: 0 11px; cursor: pointer; }
.lead-col-select { width: 42px; }
.lead-select-column { width: 42px; padding-right: 0 !important; text-align: center !important; }
.lead-select-column input, .lead-mobile-checkbox input { width: 18px; height: 18px; accent-color: var(--kx-color-brand); }
.lead-batch-bar { position: fixed; z-index: 40; right: 24px; bottom: 24px; left: calc(var(--kx-shell-sidebar-width) + 24px); display: flex; min-height: 58px; align-items: center; gap: 12px; padding: 10px 14px; border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-4); background: var(--kx-color-surface); box-shadow: var(--kx-shadow-float); color: var(--kx-color-text); }
.lead-batch-bar span { color: var(--kx-color-text-muted); font-size: 12px; }
.lead-batch-bar select { min-width: 180px; height: var(--kx-control-height); margin-left: auto; border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); }
.lead-batch-bar button { min-height: var(--kx-control-height); padding: 0 14px; border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); }
.lead-batch-bar button.primary { border-color: var(--kx-color-brand); background: var(--kx-color-brand); color: var(--kx-color-on-brand); }
.lead-mobile-select-row { display: grid; grid-template-columns: minmax(0, 1fr); align-items: start; padding: 0 var(--kx-space-3); border-bottom: 1px solid var(--kx-color-border); }
.lead-mobile-select-row.can-select { grid-template-columns: 28px minmax(0, 1fr); }
.lead-mobile-checkbox { position: relative; z-index: 1; display: grid; width: 44px; min-height: 44px; margin: 4px 0 0 -8px; place-items: center; cursor: pointer; }
.lead-mobile-select-row > :deep(.lead-mobile-row) { min-width: 0; padding-left: var(--kx-space-2); padding-right: 0; border-bottom: 0; background: transparent; }
.lead-mobile-select-row:not(.can-select) > :deep(.lead-mobile-row) { padding-left: 0; }
.lead-mobile-select-row.selected { background: var(--kx-color-brand-soft); }

@media (max-width: 767px) {
  .page-container.has-batch-selection { padding-bottom: 160px; }
  .lead-batch-bar { right: 16px; bottom: calc(var(--kx-shell-mobile-nav-height) + env(safe-area-inset-bottom) + 8px); left: 16px; flex-wrap: wrap; gap: 8px; }
  .lead-batch-bar span { display: none; }
  .lead-batch-bar select { order: 3; width: 100%; min-width: 0; margin-left: 0; }
  .lead-batch-bar strong { margin-right: auto; font-size: var(--kx-font-13); }
  .lead-batch-bar button, .lead-batch-bar select { min-height: var(--kx-mobile-control-height); }
  .lead-toolbar-select, .lead-view-save { min-height: var(--kx-mobile-control-height); }
}

.lead-all-select { height: var(--kx-control-height); min-width: 82px; max-width: 150px; padding: 0 28px 0 12px; border: 0; border-radius: var(--kx-radius-2); color: var(--kx-color-text); background: var(--kx-color-segment-selected); font-size: var(--kx-font-13); cursor: pointer; }
.lead-date-dropdown { position: relative; flex: 0 0 auto; }
.lead-date-trigger { display: inline-flex; height: var(--kx-control-height); align-items: center; gap: 6px; padding: 0 11px; border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); white-space: nowrap; cursor: pointer; }
.lead-date-trigger.active { border-color: var(--kx-color-brand); background: var(--kx-color-brand-soft); color: var(--kx-color-brand); }
.lead-date-popover { position: absolute; z-index: 20; top: 42px; right: 0; display: grid; width: 280px; gap: 12px; padding: 16px; border: 1px solid var(--kx-color-border-strong); border-radius: 12px; background: var(--kx-color-surface); box-shadow: var(--kx-shadow-float); }
.lead-date-popover strong { color: var(--kx-color-text); font-size: 14px; }
.lead-date-popover label { display: grid; grid-template-columns: 42px 1fr; align-items: center; gap: 8px; color: var(--kx-color-text-secondary); font-size: 12px; }
.lead-date-popover input { height: var(--kx-control-height); padding: 0 8px; border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-3); color: var(--kx-color-text-secondary); }
.lead-date-popover > div { display: flex; justify-content: flex-end; gap: 8px; }
.lead-filter-apply, .lead-filter-clear { height: var(--kx-control-height); padding: 0 13px; border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); cursor: pointer; }
.lead-filter-apply { border-color: var(--kx-color-brand); background: var(--kx-color-brand); color: var(--kx-color-surface); }
.lead-filter-clear { border: 0; color: var(--kx-color-text-secondary); }
.workbench-band { display: flex; align-items: center; min-height: 100px; padding: var(--kx-space-4) 0 var(--kx-space-6); border-bottom: 1px solid var(--kx-color-border); background: transparent; }

.workbench-metric { display: flex; flex: 1; min-width: 0; flex-direction: column; justify-content: center; gap: var(--kx-space-2); padding: 0 var(--kx-space-6); border-right: 1px solid var(--kx-color-border); }

.workbench-metric:first-child { padding-left: 0; }
.workbench-metric:nth-last-child(2) { border-right: 0; }
.workbench-metric span { color: var(--kx-color-text-secondary); font-size: 12px; }
.workbench-metric strong { color: var(--kx-color-text); font-size: 30px; font-weight: 600; font-variant-numeric: tabular-nums; line-height: 1.2; letter-spacing: -.5px; }
.workbench-metric strong.danger { color: var(--kx-color-danger); }
.workbench-metric.danger { background: transparent; }
.workbench-refresh { width: var(--kx-control-height); height: var(--kx-control-height); flex: 0 0 auto; border: 0; border-radius: var(--kx-radius-3); background: transparent; color: var(--kx-color-text-secondary); cursor: pointer; }

.workbench-task-panel { margin-top: var(--kx-space-3); color: var(--kx-color-text-secondary); }

.workbench-task-panel > summary { display: flex; min-height: 44px; align-items: center; gap: var(--kx-space-2); list-style: none; font-size: var(--kx-font-13); cursor: pointer; }
.workbench-task-panel > summary::-webkit-details-marker { display: none; }
.workbench-task-panel > summary > .van-icon:first-child { color: var(--kx-color-warning); }
.workbench-task-panel > summary strong { font-weight: 500; }
.workbench-task-disclosure { margin-left: auto; }
.workbench-task-panel[open] > summary > .van-icon:last-child { transform: rotate(180deg); }
.workbench-task-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--kx-space-2); padding-bottom: var(--kx-space-2); }
.workbench-error { min-height: 44px; margin-top: var(--kx-space-2); border: 0; border-radius: var(--kx-radius-3); background: var(--kx-color-danger-soft); color: var(--kx-color-danger); text-align: left; padding: 0 var(--kx-space-3); }
.workbench-task { display: flex; min-width: 0; align-items: center; gap: var(--kx-space-2); min-height: 44px; padding: var(--kx-space-2) var(--kx-space-3); border: 1px solid var(--kx-color-border); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); cursor: pointer; }
.workbench-task > span:nth-child(2) { min-width: 0; flex: 1; text-align: left; overflow-wrap: anywhere; }
.workbench-task time { color: var(--kx-color-text-secondary); font-size: var(--kx-font-12); }
.workbench-task-dot { width: 6px; height: 6px; flex: 0 0 auto; border-radius: 50%; background: var(--kx-color-text-secondary); }
.workbench-task-dot.high { background: var(--kx-color-danger); }

.workbench-mobile { margin: 0 var(--kx-space-4) var(--kx-space-4); background: transparent; }
.workbench-mobile-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--kx-space-2); padding: var(--kx-space-3) 0 var(--kx-space-6); border-bottom: 1px solid var(--kx-color-border); }
.workbench-mobile-summary > div { display: flex; min-width: 0; flex-direction: column; gap: var(--kx-space-2); padding-left: var(--kx-space-2); border-left: 1px solid var(--kx-color-border); }
.workbench-mobile-summary > div:first-child { padding-left: 0; border-left: 0; }
.workbench-mobile-summary strong { color: var(--kx-color-text); font-size: var(--kx-font-26); font-weight: 600; font-variant-numeric: tabular-nums; line-height: 1.2; }
.workbench-mobile-summary strong.danger { color: var(--kx-color-danger); }
.workbench-mobile-summary span { color: var(--kx-color-text-muted); font-size: var(--kx-font-12); line-height: 1.4; overflow-wrap: anywhere; }
.workbench-mobile-tasks > summary { display: flex; min-height: var(--kx-mobile-control-height); align-items: center; gap: var(--kx-space-2); padding: var(--kx-space-1) 0; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); list-style: none; cursor: pointer; }
.workbench-mobile-tasks > summary::-webkit-details-marker { display: none; }
.workbench-mobile-tasks > summary > span:nth-child(2) { margin-left: auto; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.workbench-mobile-tasks > summary .van-icon { transition: transform var(--kx-motion-fast); }
.workbench-mobile-tasks[open] > summary .van-icon { transform: rotate(180deg); }
.workbench-mobile-task { display: flex; width: 100%; min-height: var(--kx-mobile-control-height); align-items: center; justify-content: space-between; gap: var(--kx-space-2); padding: var(--kx-space-2) 0; border: 0; border-top: 1px solid var(--kx-color-border); background: transparent; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); text-align: left; }
.workbench-mobile-retry { width: 100%; min-height: var(--kx-mobile-control-height); border: 0; border-top: 1px solid var(--kx-color-border); background: var(--kx-color-danger-soft); color: var(--kx-color-danger); }

.lead-status-segment { display: flex; align-items: center; align-self: flex-start; gap: 2px; padding: var(--kx-space-1); max-width: 100%; flex-wrap: wrap; border: 0; border-radius: var(--kx-radius-3); background: var(--kx-color-segment); }

.lead-status-segment button { min-height: var(--kx-control-height); padding: 0 var(--kx-space-3); border: 0; border-radius: var(--kx-radius-2); background: transparent; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); font-weight: 400; }
.lead-status-segment button.active, .lead-all-select { border-color: transparent; background: var(--kx-color-segment-selected); color: var(--kx-color-text); box-shadow: var(--kx-shadow-selected); font-weight: 600; }
.lead-toolbar-side { display: flex; flex-wrap: wrap; align-items: center; gap: var(--kx-space-3); width: 100%; min-width: 0; }

.lead-view-actions, .lead-query-actions { display: flex; min-width: 0; align-items: center; gap: var(--kx-space-2); }
.lead-query-actions .lead-desktop-search { order: -1; }
.lead-query-actions { justify-content: flex-start; flex: 1 1 540px; order: 1; }
.lead-view-actions { flex: 0 1 auto; flex-wrap: wrap; order: 2; }
.lead-view-actions > span { flex-basis: 100%; font-size: 12px; color: var(--kx-color-text-secondary); }

.lead-desktop-search {
  width: auto;
  min-width: 180px;
  flex: 1 1 320px;
  padding: 0;
  background: transparent;
}

.lead-refresh-button {
  height: var(--kx-control-height);
}

.lead-table-panel { display: flex; flex-direction: column; overflow: hidden; border: 0; border-radius: 0 0 var(--kx-card-radius) var(--kx-card-radius); box-shadow: none; }

.lead-table-panel > .kx-desktop-panel-header { min-height: 56px; padding: var(--kx-space-3) var(--kx-space-4); gap: var(--kx-space-3); }
.lead-table-panel .kx-desktop-panel-header > div:first-child { display: flex; align-items: baseline; flex-wrap: wrap; gap: var(--kx-space-2); }
.lead-table-panel .kx-desktop-panel-title { font-size: var(--kx-font-13); font-weight: 500; }
.lead-table-panel .kx-desktop-panel-subtitle { margin: 0; font-size: var(--kx-font-12); }
.lead-filter-labels {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}

.lead-stats-range {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: var(--kx-control-height);
  padding: 4px 10px;
  border: 1px solid var(--kx-color-border);
  border-radius: var(--kx-radius-1);
  background: var(--kx-color-brand-soft);
  color: var(--kx-color-brand-hover);
  font-size: 12px;
  cursor: pointer;
}

.lead-table-scroll {
  overflow-x: auto;
  max-height: max(320px, calc(100dvh - 340px));
}

.lead-table { width: 100%; min-width: 960px; table-layout: fixed; font-size: var(--kx-font-14); }

.lead-table th {
  position: sticky;
  z-index: 3;
  top: 0;
  height: 42px;
}

.lead-table td { height: 84px; padding-top: var(--kx-space-4); padding-bottom: var(--kx-space-4); }

.lead-stage-status { display: flex; flex-direction: column; align-items: flex-start; gap: var(--kx-space-1); min-width: 0; }

.lead-col-customer { width: 21%; }

.lead-col-progress { width: 14%; }

.lead-col-source { width: 20%; }

.lead-col-owner { width: 15%; }

.lead-col-followup { width: 16%; }

.lead-col-created { width: 14%; }

.lead-customer-cell {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 0;
}

.lead-title-link { max-width: 100%; color: var(--kx-color-text); font-size: var(--kx-font-14); font-weight: 600; overflow-wrap: anywhere; }

.lead-customer-meta {
  max-width: 100%;
  overflow: hidden;
  color: var(--kx-color-text-secondary);
  font-size: 12px;
  line-height: 18px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lead-progress-cell,
.lead-source-cell,
.lead-owner-cell,
.lead-followup-cell {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 7px;
}

.lead-source-text { display: block; color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); font-weight: 400; line-height: 1.5; overflow-wrap: anywhere; }

.lead-secondary-text {
  display: -webkit-box;
  overflow: hidden;
  color: var(--kx-color-text-secondary);
  font-size: 12px;
  line-height: 18px;
  overflow-wrap: anywhere;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.lead-stage-pill { display: inline-flex; align-items: center; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); font-weight: 400; }

.lead-stage-pill.invitation { color: var(--kx-color-text-muted); }

.lead-stage-pill.negotiation { color: var(--kx-color-text-muted); }

.lead-star-row {
  display: flex;
  min-height: 18px;
  align-items: center;
}

.lead-star-row.empty {
  color: var(--kx-color-text-muted);
  font-size: 12px;
}

.lead-owner-primary {
  overflow: hidden;
  color: var(--kx-color-text-secondary);
  font-weight: 400;
  line-height: 20px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lead-followup-cell span {
  font-weight: 600;
  line-height: 20px;
  white-space: nowrap;
}

.lead-followup-cell time,
.lead-created-cell time {
  color: var(--kx-color-text-muted);
  font-size: var(--kx-font-12);
  font-variant-numeric: tabular-nums;
  line-height: 16px;
  white-space: nowrap;
}

.lead-followup-cell.normal span {
  color: var(--kx-color-text-secondary);
}

.lead-followup-cell.warning span {
  color: var(--kx-color-warning);
}

.lead-followup-cell.danger span {
  color: var(--kx-color-danger);
}

.lead-followup-cell.muted span {
  color: var(--kx-color-text-muted);
  font-weight: 500;
}

.lead-created-cell {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.lead-created-cell > div {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 6px;
}

.lead-created-cell > div > span {
  color: var(--kx-color-text-secondary);
  white-space: nowrap;
}

.lead-row-arrow {
  flex: 0 0 auto;
  color: var(--kx-color-border-strong);
  font-size: 14px;
  opacity: 0;
  transform: translateX(-4px);
  transition: opacity var(--kx-motion-fast), transform var(--kx-motion-fast), color var(--kx-motion-fast);
}

.lead-clickable-row {
  position: relative;
  cursor: pointer;
  outline: none;
}

.lead-clickable-row:hover .lead-row-arrow,
.lead-clickable-row:focus-visible .lead-row-arrow {
  color: var(--kx-color-brand);
  opacity: 1;
  transform: translateX(0);
}

.lead-row-selected td { background: var(--kx-color-brand-soft); }

.lead-row-overdue td:first-child {
  box-shadow: inset 3px 0 0 var(--kx-color-danger);
}

.lead-clickable-row:focus-visible td {
  background: var(--kx-color-brand-soft);
  box-shadow: inset 0 0 0 1px var(--kx-color-border);
}

.lead-table-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 52px;
  padding: 0 14px;
  border-top: 1px solid var(--kx-color-border);
  color: var(--kx-color-text-secondary);
  font-size: 13px;
}

.lead-table-panel :deep(.van-empty) {
  min-height: 280px;
}

.empty-lead-action { margin-top: 8px; }

.search-row {
  background: var(--kx-color-surface);
}

.search-row :deep(.van-search) {
  padding: 0;
}

.list-state {
  padding: 48px 0;
}

.lead-filter-reset-all { min-height: var(--kx-control-height); padding: 0 6px; border: 0; background: transparent; color: var(--kx-color-text-muted); font-size: var(--kx-font-12); cursor: pointer; }
.lead-mobile-heading { display: flex; min-height: 96px; align-items: center; justify-content: space-between; gap: var(--kx-space-3); padding: var(--kx-space-6) var(--kx-space-4) var(--kx-space-3); background: var(--kx-color-canvas); }
.lead-mobile-heading > div { min-width: 0; }
.lead-mobile-heading h1 { margin: 0; color: var(--kx-color-text); font-size: var(--kx-mobile-title-size); line-height: 1.2; font-weight: 700; letter-spacing: -.7px; }
.lead-mobile-heading p { margin: var(--kx-space-1) 0 0; color: var(--kx-color-text-muted); font-size: var(--kx-font-13); }
.lead-mobile-heading > button { width: 44px; height: 44px; flex: 0 0 auto; border: 0; border-radius: 50%; background: var(--kx-color-surface); color: var(--kx-color-brand); font-size: var(--kx-font-20); }
.lead-mobile-controls { margin: 0 var(--kx-space-4); }
.lead-mobile-surface { margin: 0 var(--kx-space-4) var(--kx-space-4); overflow: hidden; border-radius: var(--kx-card-radius); background: var(--kx-color-surface); }
.lead-mobile-status-row { display: flex; gap: var(--kx-space-2); margin-top: var(--kx-space-3); }
.lead-mobile-status-segment { display: flex; flex: 1; min-width: 0; padding: 3px; border-radius: var(--kx-mobile-control-radius); background: var(--kx-color-mobile-fill); }
.lead-mobile-status-segment button { flex: 1; min-width: 0; min-height: 44px; padding: 0 var(--kx-space-1); border: 0; border-radius: 9px; background: transparent; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); }
.lead-mobile-status-segment button[aria-pressed="true"] { background: var(--kx-color-surface); color: var(--kx-color-text); box-shadow: var(--kx-shadow-selected); font-weight: 600; }
.lead-mobile-status-more { display: inline-flex; min-height: 44px; align-items: center; gap: var(--kx-space-1); padding: 0 var(--kx-space-2); border: 0; border-radius: var(--kx-mobile-control-radius); background: transparent; color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); }
.lead-mobile-status-more.selected { background: var(--kx-color-brand-soft); color: var(--kx-color-brand); }
.lead-mobile-results { display: flex; flex-wrap: wrap; justify-content: space-between; gap: var(--kx-space-1) var(--kx-space-2); padding: var(--kx-space-1) var(--kx-space-1) var(--kx-space-3); color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.lead-mobile-current-view { max-width: 100%; overflow-wrap: anywhere; color: var(--kx-color-brand); }
.lead-mobile-search-row { margin: 0; padding: 0; }
.lead-mobile-search-row :deep(.van-search) { width: 100%; min-width: 0; padding: 0; }
.lead-mobile-tools { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.6fr) minmax(0, 1fr); margin: var(--kx-space-1) 0; }
.lead-mobile-tools > button { display: inline-flex; min-width: 0; min-height: 44px; align-items: center; justify-content: center; gap: 5px; padding: var(--kx-space-1); border: 0; border-radius: var(--kx-mobile-control-radius); background: transparent; color: var(--kx-color-text-secondary); font-size: var(--kx-font-14); }
.lead-mobile-tools > button:first-child { justify-content: flex-start; }
.lead-mobile-tools > button:last-child { justify-content: flex-end; }
.lead-mobile-tools > button .van-icon { color: var(--kx-color-brand); font-size: var(--kx-font-16); }
.lead-mobile-tools > button.active { color: var(--kx-color-brand); }
.lead-mobile-tools small { display: grid; min-width: 18px; min-height: 18px; place-items: center; border-radius: 9px; background: var(--kx-color-brand); color: var(--kx-color-on-brand); font-size: var(--kx-font-12); }
.lead-mobile-filter-labels { display: flex; gap: var(--kx-space-2); overflow-x: auto; padding: 0 0 var(--kx-space-2); }
.lead-mobile-filter-labels .lead-stats-range { flex: 0 0 auto; }
.lead-mobile-list { background: var(--kx-color-surface); }
.lead-mobile-menu { max-height: 85dvh; overflow-y: auto; padding: var(--kx-space-4) var(--kx-space-4) calc(var(--kx-space-6) + env(safe-area-inset-bottom)); background: var(--kx-color-canvas); border-radius: var(--kx-mobile-sheet-radius) var(--kx-mobile-sheet-radius) 0 0; }
.lead-mobile-menu-heading { display: flex; align-items: center; justify-content: space-between; gap: var(--kx-space-3); margin-bottom: var(--kx-space-4); }
.lead-mobile-menu-heading h2 { margin: 0; color: var(--kx-color-text); font-size: var(--kx-mobile-heading-size); font-weight: 600; }
.lead-mobile-menu-heading > button { display: grid; width: 44px; height: 44px; place-items: center; border: 0; border-radius: 50%; background: var(--kx-color-mobile-fill); color: var(--kx-color-text-secondary); font-size: var(--kx-font-16); }
.lead-mobile-menu-options { overflow: hidden; border-radius: var(--kx-mobile-control-radius); background: var(--kx-color-surface); }
.lead-mobile-menu-options > button { display: flex; width: 100%; min-height: 52px; align-items: center; justify-content: space-between; gap: var(--kx-space-3); padding: var(--kx-space-3) var(--kx-space-4); border: 0; background: transparent; color: var(--kx-color-text); font-size: var(--kx-font-16); text-align: left; }
.lead-mobile-menu-options > button + button { border-top: 1px solid var(--kx-color-mobile-separator); }
.lead-mobile-menu-options > button > span { min-width: 0; overflow-wrap: anywhere; }
.lead-mobile-menu-options .van-icon, .lead-mobile-view-options button { color: var(--kx-color-brand); }
.lead-mobile-menu-options > button[aria-pressed="true"] { font-weight: 600; }
.lead-mobile-menu-options > button:disabled { color: var(--kx-color-text-muted); }
.lead-mobile-menu-note { margin: 0 var(--kx-space-2) var(--kx-space-3); color: var(--kx-color-text-muted); font-size: var(--kx-font-13); line-height: 1.5; }
.lead-mobile-view-options { margin-top: var(--kx-space-4); }
.lead-mobile-view-options + .lead-mobile-menu-note { margin-top: var(--kx-space-3); }
.lead-filter-popup { width: min(560px, 100%); max-height: 88dvh; overflow-y: auto; padding: var(--kx-space-4) var(--kx-space-4) calc(var(--kx-space-6) + env(safe-area-inset-bottom)); }
.lead-filter-popup-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--kx-space-3); padding-bottom: var(--kx-space-4); border-bottom: 1px solid var(--kx-color-border); }
.lead-filter-popup-heading > div { display: grid; gap: var(--kx-space-1); }
.lead-filter-popup-heading strong { font-size: var(--kx-font-16); }
.lead-filter-popup-heading span { color: var(--kx-color-text-muted); font-size: var(--kx-font-12); }
.lead-filter-popup-heading button { width: 44px; height: 44px; border: 0; background: transparent; color: var(--kx-color-text-muted); }
.lead-filter-field, .lead-filter-date-fields label { display: grid; gap: var(--kx-space-2); color: var(--kx-color-text-secondary); font-size: var(--kx-font-13); }
.lead-filter-field { margin-top: var(--kx-space-4); }
.lead-filter-field input, .lead-filter-field select, .lead-filter-date-fields input { width: 100%; height: 44px; min-width: 0; padding: 0 var(--kx-space-3); border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); color: var(--kx-color-text); }
.lead-filter-date-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--kx-space-3); margin-top: var(--kx-space-4); }
.lead-filter-popup-actions { display: grid; grid-template-columns: 1fr 2fr; gap: var(--kx-space-3); margin-top: var(--kx-space-6); }
.lead-filter-popup-actions button { height: 44px; border: 1px solid var(--kx-color-border-strong); border-radius: var(--kx-radius-3); background: var(--kx-color-surface); color: var(--kx-color-text-secondary); }
.lead-filter-popup-actions .primary { border-color: var(--kx-color-brand); background: var(--kx-color-brand); color: var(--kx-color-surface); }

@media (max-width: 1100px) {

  .lead-view-actions { justify-content: flex-start; }
  .lead-query-actions { flex-basis: 100%; }

  .lead-desktop-search {
    width: 100%;
    min-width: 0;
  }

  .lead-refresh-button {
    width: fit-content;
  }

  .lead-table {
    min-width: 960px;
  }
}

@media (max-width: 767px) {
  .lead-mobile-filter-labels button { min-height: var(--kx-mobile-control-height); }
  .lead-toolbar-select, .lead-filter-field input, .lead-filter-field select, .lead-filter-date-fields input { font-size: var(--kx-font-16); }
}

@media (max-width: 820px) {
  .workbench-task-list { grid-template-columns: 1fr; }
}
@media (max-width: 359px) {
  .workbench-mobile-summary span { min-height: 36px; }
}
</style>
