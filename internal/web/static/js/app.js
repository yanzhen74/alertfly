/**
 * AlertFly 前端业务逻辑
 * 依赖: Layui 2.9.x
 */
(function () {
  'use strict';

  // ==================== 页面检测 ====================
  var isIndexPage = !!document.getElementById('msgTable');
  var isSettingsPage = !!document.getElementById('settingsForm');

  // ==================== 工具函数 ====================
  function formatTime(ts) {
    if (!ts) return '-';
    var d = new Date(ts);
    if (isNaN(d.getTime())) return ts;
    var pad = function (n) { return n < 10 ? '0' + n : '' + n; };
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) +
      ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds());
  }

  function levelHtml(level) {
    var cls = 'level-info';
    if (level === 'error') cls = 'level-error';
    else if (level === 'warn') cls = 'level-warn';
    return '<span class="level-tag ' + cls + '">' + level + '</span>';
  }

  function escapeHtml(str) {
    if (!str) return '';
    return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function tryFormatJson(str) {
    try {
      var obj = JSON.parse(str);
      return JSON.stringify(obj, null, 2);
    } catch (e) {
      return str;
    }
  }

  // ==================== 首页逻辑 ====================
  function initIndexPage(layui) {
    var table = layui.table;
    var form = layui.form;
    var laydate = layui.laydate;
    var laypage = layui.laypage;
    var layer = layui.layer;

    // 当前分页参数
    var currentPage = 1;
    var pageSize = 15;

    // 日期选择器
    laydate.render({ elem: '#startTime', type: 'datetime' });
    laydate.render({ elem: '#endTime', type: 'datetime' });

    // 获取过滤参数
    function getFilterParams() {
      var data = form.val('filterForm');
      return {
        keyword: data.keyword || '',
        level: data.level || '',
        source: data.source || '',
        mission: data.mission || '',
        sender: data.sender || '',
        subtype: data.subtype || '',
        startTime: data.startTime || '',
        endTime: data.endTime || ''
      };
    }

    // 渲染表格
    function renderTable(page) {
      if (page) currentPage = page;
      var filter = getFilterParams();

      var params = {
        page: currentPage,
        pageSize: pageSize,
        level: filter.level,
        source: filter.source,
        keyword: filter.keyword,
        mission: filter.mission,
        sender: filter.sender,
        subtype: filter.subtype,
        startTime: filter.startTime,
        endTime: filter.endTime
      };

      // 构造查询字符串
      var qs = [];
      for (var k in params) {
        if (params[k] !== '') qs.push(encodeURIComponent(k) + '=' + encodeURIComponent(params[k]));
      }
      var url = '/api/messages?' + qs.join('&');

      // 使用 fetch 请求数据
      fetch(url).then(function (res) { return res.json(); }).then(function (result) {
        var count = result.count || 0;
        var data = result.data || [];

        // 渲染表格
        table.render({
          elem: '#msgTable',
          data: data,
          cols: [[
            { field: 'received_at', title: '时间', width: 170, templet: function (d) { return formatTime(d.received_at); } },
            { field: 'source', title: '来源', width: 80 },
            { field: 'level', title: '级别', width: 80, templet: function (d) { return levelHtml(d.level); } },
            { field: 'title', title: '标题', minWidth: 180, templet: function (d) { return '<span class="cell-ellipsis" title="' + escapeHtml(d.title) + '">' + escapeHtml(d.title) + '</span>'; } },
            { field: 'mission', title: '任务', width: 120 },
            { field: 'sender', title: '发送者', width: 110 },
            { field: 'content', title: '内容摘要', minWidth: 200, templet: function (d) {
              var summary = d.content || '';
              if (summary.length > 60) summary = summary.substring(0, 60) + '...';
              return '<span title="' + escapeHtml(d.content) + '">' + escapeHtml(summary) + '</span>';
            }}
          ]],
          page: false,
          limit: pageSize,
          even: true,
          skin: 'line',
          done: function () {
            // 行点击展开详情
            bindRowToggle();
          }
        });

        // 渲染分页
        laypage.render({
          elem: 'pager',
          count: count,
          limit: pageSize,
          curr: currentPage,
          layout: ['count', 'prev', 'page', 'next', 'limit', 'skip'],
          limits: [10, 15, 30, 50],
          jump: function (obj, first) {
            currentPage = obj.curr;
            pageSize = obj.limit;
            if (!first) {
              renderTable(obj.curr);
            }
          }
        });
      }).catch(function (err) {
        console.error('请求数据失败:', err);
      });
    }

    // 行点击展开详情
    function bindRowToggle() {
      var rows = document.querySelectorAll('.layui-table-body tr');
      rows.forEach(function (tr) {
        tr.classList.add('row-toggle');
        tr.onclick = function (e) {
          // 忽略链接和按钮点击
          if (e.target.tagName === 'A' || e.target.tagName === 'BUTTON') return;
          var idx = tr.getAttribute('data-index');
          var next = tr.nextElementSibling;
          if (next && next.classList.contains('row-detail') && next.getAttribute('data-idx') === idx) {
            next.remove();
            return;
          }
          // 移除其它展开行
          document.querySelectorAll('.row-detail').forEach(function (el) { el.remove(); });

          // 获取行数据
          var tableData = table.cache['msgTable'];
          var rowData = null;
          if (tableData && tableData[idx]) {
            rowData = tableData[idx];
          }
          if (!rowData) return;

          var detailTr = document.createElement('tr');
          detailTr.className = 'row-detail';
          detailTr.setAttribute('data-idx', idx);
          var tdCount = tr.querySelectorAll('td').length;
          detailTr.innerHTML = '<td colspan="' + tdCount + '"><div class="row-detail-inner">' +
            '<p style="margin-bottom:8px;color:#666;"><strong>来源:</strong> ' + escapeHtml(rowData.source) +
            ' &nbsp;|&nbsp; <strong>级别:</strong> ' + levelHtml(rowData.level) +
            ' &nbsp;|&nbsp; <strong>任务:</strong> ' + escapeHtml(rowData.mission || '-') +
            ' &nbsp;|&nbsp; <strong>发送者:</strong> ' + escapeHtml(rowData.sender || '-') +
            ' &nbsp;|&nbsp; <strong>时间:</strong> ' + formatTime(rowData.received_at) + '</p>' +
            '<pre>' + escapeHtml(tryFormatJson(rowData.content || '')) + '</pre>' +
            '</div></td>';
          tr.after(detailTr);
        };
      });
    }

    // 查询按钮
    document.getElementById('btnQuery').onclick = function () {
      renderTable(1);
    };

    // 重置按钮
    document.getElementById('btnReset').onclick = function () {
      document.getElementById('filterForm').reset();
      form.render('select');
      renderTable(1);
    };

    // 初始加载
    renderTable(1);
  }

  // ==================== 设置页逻辑 ====================
  function initSettingsPage(layui) {
    var form = layui.form;
    var layer = layui.layer;

    // Redis/Kafka 启用开关切换：显示/隐藏对应配置区域
    form.on('switch(redis_enabled)', function (data) {
      toggleFieldset('redisFieldset', data.elem.checked);
      ensureAtLeastOneEnabled();
    });

    form.on('switch(kafka_enabled)', function (data) {
      toggleFieldset('kafkaFieldset', data.elem.checked);
      ensureAtLeastOneEnabled();
    });

    function toggleFieldset(fieldsetId, enabled) {
      var fs = document.getElementById(fieldsetId);
      if (enabled) {
        fs.style.display = '';
        fs.style.opacity = '1';
        fs.style.pointerEvents = 'auto';
      } else {
        fs.style.display = 'none';
      }
    }

    function ensureAtLeastOneEnabled() {
      var redisSwitch = document.querySelector('input[name="redis_enabled"]');
      var kafkaSwitch = document.querySelector('input[name="kafka_enabled"]');
      if (!redisSwitch.checked && !kafkaSwitch.checked) {
        layer.msg('至少需要启用一个消费者', { icon: 0 });
      }
    }

    function isRedisEnabled() {
      var el = document.querySelector('input[name="redis_enabled"]');
      return el && el.checked;
    }

    function isKafkaEnabled() {
      var el = document.querySelector('input[name="kafka_enabled"]');
      return el && el.checked;
    }

    // 加载配置
    function loadConfig() {
      fetch('/api/config').then(function (res) { return res.json(); }).then(function (result) {
        var data = result.data || result;
        // 填充表单
        form.val('settingsForm', {
          redis_enabled: data.redis && data.redis.enabled ? true : false,
          redis_addr: (data.redis && data.redis.addr) || '',
          redis_password: (data.redis && data.redis.password) || '',
          redis_db: (data.redis && data.redis.db != null) ? String(data.redis.db) : '0',
          redis_channel: (data.redis && data.redis.channel) || '',
          redis_stream: (data.redis && data.redis.stream) || '',
          redis_consumer_group: (data.redis && data.redis.consumer_group) || '',
          redis_mode: (data.redis && data.redis.mode) || 'pubsub',
          kafka_enabled: data.kafka && data.kafka.enabled ? true : false,
          kafka_brokers: (data.kafka && data.kafka.brokers) ? data.kafka.brokers.join(',') : '',
          kafka_topic: (data.kafka && data.kafka.topic) || '',
          kafka_group_id: (data.kafka && data.kafka.group_id) || '',
          storage_db_path: (data.storage && data.storage.db_path) || '',
          storage_retention_days: (data.storage && data.storage.retention_days != null) ? String(data.storage.retention_days) : '',
          storage_max_records: (data.storage && data.storage.max_records != null) ? String(data.storage.max_records) : '',
          notifier_enabled: (data.notifier && data.notifier.enabled) ? true : false,
          updater_enabled: (data.updater && data.updater.enabled) ? true : false,
          updater_check_url: (data.updater && data.updater.check_url) || '',
          updater_interval: (data.updater && data.updater.interval != null) ? String(Math.round(data.updater.interval / 60000000000)) : ''
        });

        // 根据 enabled 状态切换配置区域显示
        toggleFieldset('redisFieldset', data.redis && data.redis.enabled);
        toggleFieldset('kafkaFieldset', data.kafka && data.kafka.enabled);
      }).catch(function (err) {
        console.error('加载配置失败:', err);
        layer.msg('加载配置失败', { icon: 2 });
      });
    }

    // 保存配置
    document.getElementById('btnSave').onclick = function () {
      var data = form.val('settingsForm');

      // 至少启用一个消费者
      var redisOn = data.redis_enabled === 'on';
      var kafkaOn = data.kafka_enabled === 'on';
      if (!redisOn && !kafkaOn) {
        layer.msg('至少需要启用一个消费者（Redis 或 Kafka）', { icon: 0 });
        return;
      }

      // 组装提交数据
      var payload = {
        redis: {
          enabled: redisOn,
          addr: data.redis_addr,
          password: data.redis_password,
          db: parseInt(data.redis_db) || 0,
          channel: data.redis_channel,
          stream: data.redis_stream,
          consumer_group: data.redis_consumer_group,
          mode: data.redis_mode
        },
        kafka: {
          enabled: kafkaOn,
          brokers: data.kafka_brokers ? data.kafka_brokers.split(',').map(function(s) { return s.trim(); }).filter(Boolean) : [],
          topic: data.kafka_topic,
          group_id: data.kafka_group_id
        },
        storage: {
          db_path: data.storage_db_path,
          retention_days: parseInt(data.storage_retention_days) || 0,
          max_records: parseInt(data.storage_max_records) || 0
        },
        notifier: {
          enabled: data.notifier_enabled === 'on'
        },
        updater: {
          enabled: data.updater_enabled === 'on',
          check_url: data.updater_check_url,
          interval: (parseInt(data.updater_interval) || 0) * 60000000000
        }
      };

      fetch('/api/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      }).then(function (res) { return res.json(); }).then(function (result) {
        if (result.code === 0 || result.ok) {
          layer.msg('保存成功', { icon: 1 });
        } else {
          layer.msg(result.msg || '保存失败', { icon: 2 });
        }
      }).catch(function (err) {
        console.error('保存配置失败:', err);
        layer.msg('保存失败', { icon: 2 });
      });
    };

    // 取消按钮
    document.getElementById('btnCancel').onclick = function () {
      loadConfig();
      layer.msg('已还原配置', { icon: 1 });
    };

    // 初始加载
    loadConfig();
  }

  // ==================== Layui 初始化 ====================
  layui.use(['table', 'form', 'laydate', 'laypage', 'layer', 'element'], function () {
    var layui = this;

    if (isIndexPage) {
      initIndexPage(layui);
    }

    if (isSettingsPage) {
      initSettingsPage(layui);
      // 立即检查更新按钮
      var btnCheckUpdate = document.getElementById('btnCheckUpdate');
      var updateStatus = document.getElementById('updateStatus');
      if (btnCheckUpdate) {
        btnCheckUpdate.onclick = function() {
          btnCheckUpdate.disabled = true;
          updateStatus.innerHTML = '<i class="layui-icon layui-icon-loading layui-anim layui-anim-rotate layui-anim-loop"></i> 正在检查...';
          var xhr = new XMLHttpRequest();
          xhr.open('POST', '/api/update/check', true);
          xhr.onreadystatechange = function() {
            if (xhr.readyState === 4) {
              btnCheckUpdate.disabled = false;
              if (xhr.status === 200) {
                var res = JSON.parse(xhr.responseText);
                if (res.code === 0) {
                  updateStatus.innerHTML = '<span style="color:#5FB878;">' + res.msg + '</span>';
                } else {
                  updateStatus.innerHTML = '<span style="color:#FF5722;">' + res.msg + '</span>';
                }
              } else {
                updateStatus.innerHTML = '<span style="color:#FF5722;">请求失败</span>';
              }
            }
          };
          xhr.send();
        };
      }
    }
  });

})();
