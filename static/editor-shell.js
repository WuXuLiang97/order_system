(function (window, $) {
    'use strict';

    var dirty = false;

    function queryParams() {
        return new URLSearchParams(window.location.search || '');
    }

    function entityID(name) {
        var value = parseInt(queryParams().get(name || 'id') || '0', 10) || 0;
        return value > 0 ? value : 0;
    }

    function parseJSONResponse(xhr, fallback) {
        try {
            var data = JSON.parse(xhr.responseText || '{}');
            return data.error || data.message || fallback;
        } catch (e) {
            return xhr.responseText || fallback;
        }
    }

    function noticeHost() {
        var host = $('#editorNotice');
        if (host.length) return host;
        host = $('<div id="editorNotice"></div>');
        var page = $('#pageContent');
        if (page.length) host.prependTo(page);
        else host.prependTo(document.body);
        return host;
    }

    function toastHost() {
        var host = $('#editorToastHost');
        if (host.length) return host;
        host = $('<div id="editorToastHost" aria-live="polite" aria-atomic="true"></div>').appendTo(document.body);
        host.css({
            position: 'fixed',
            top: '1rem',
            right: '1rem',
            zIndex: 1095,
            width: 'min(420px, calc(100vw - 2rem))',
            pointerEvents: 'none'
        });
        return host;
    }

    function notify(message, type) {
        var cls = type === 'error' ? 'danger' : (type || 'success');
        var text = String(message == null ? '' : message);
        var host = noticeHost();
        host.empty().append(
            $('<div>', { 'class': 'alert alert-' + cls + ' alert-dismissible fade show mb-3', role: 'alert' })
                .append($('<span>').text(text))
                .append('<button type="button" class="btn-close" data-bs-dismiss="alert" aria-label="关闭"></button>')
        );

        var toast = $('<div>', { 'class': 'alert alert-' + cls + ' shadow-sm mb-2', role: 'status' })
            .css({ pointerEvents: 'auto', marginBottom: '.5rem' })
            .append($('<span>').text(text))
            .appendTo(toastHost());
        window.setTimeout(function () {
            toast.fadeOut(180, function () { $(this).remove(); });
        }, cls === 'danger' ? 6500 : 3500);
    }

    function setBusy(target, busy, busyText) {
        var element = target && target.jquery ? target : $(target || []);
        element.each(function () {
            var el = $(this);
            if (busy) {
                if (el.data('editorOriginalHtml') === undefined) {
                    el.data('editorOriginalHtml', el.html());
                }
                el.prop('disabled', true).attr('aria-busy', 'true');
                if (el.is('button')) {
                    el.html('<span class="spinner-border spinner-border-sm me-1" aria-hidden="true"></span>' + (busyText || '处理中...'));
                }
            } else {
                el.prop('disabled', false).removeAttr('aria-busy');
                if (el.data('editorOriginalHtml') !== undefined) {
                    el.html(el.data('editorOriginalHtml')).removeData('editorOriginalHtml');
                }
            }
        });
    }

    function focusField(selector) {
        var field = $(selector).first();
        if (!field.length) return;
        field.trigger('focus');
        if (field.offset()) {
            $('html, body').stop(true).animate({ scrollTop: Math.max(0, field.offset().top - 150) }, 180);
        }
    }

    function markDirty() {
        dirty = true;
    }

    function markClean() {
        dirty = false;
    }

    function save(options) {
        options = options || {};
        var request = $.ajax({
            url: options.url,
            method: options.method || 'POST',
            contentType: 'application/json',
            data: options.data === undefined ? undefined : JSON.stringify(options.data)
        });

        request.done(function (response) {
            markClean();
            if (typeof options.success === 'function') options.success(response);
        });
        request.fail(function (xhr) {
            var prefix = options.errorMessage || '保存失败：';
            notify(prefix + parseJSONResponse(xhr, '请求失败'), 'error');
            if (options.errorField) focusField(options.errorField);
        });
        return request;
    }

    function bindForm(formSelector, submitHandler, options) {
        options = options || {};
        var form = $(formSelector);
        if (!form.length || typeof submitHandler !== 'function') return;

        form.attr('data-editor-form', 'true');
        form.on('input change', 'input, select, textarea', markDirty);
        form.on('submit.editorShell', function (event) {
            event.preventDefault();
            if (form.data('editorSubmitting')) return;

            var result = submitHandler.call(form[0], event, form);
            if (result === false || result === undefined || result === null) return;
            if (typeof result.always !== 'function') return;

            form.data('editorSubmitting', true);
            setBusy(options.button || form.find('[type="submit"]').last(), true, options.busyText || '保存中...');
            $.when(result).always(function () {
                form.removeData('editorSubmitting');
                setBusy(options.button || form.find('[type="submit"]').last(), false);
            });
        });
    }

    function syncEditorSummaryHeight() {
        var summary = $('.editor-summary').first();
        if (!summary.length) return;
        var update = function () {
            document.documentElement.style.setProperty('--editor-summary-height', summary.outerHeight() + 'px');
        };
        update();
        if (window.ResizeObserver) {
            new ResizeObserver(update).observe(summary[0]);
        } else {
            $(window).on('resize', update);
        }
    }

    function initSectionNav() {
        $(document).on('click', '.editor-nav [data-editor-section]', function (event) {
            var target = $($(this).attr('data-editor-section'));
            if (!target.length) return;
            event.preventDefault();
            $('html, body').animate({ scrollTop: Math.max(0, target.offset().top - 82) }, 180);
            $('.editor-nav .nav-link').removeClass('active');
            $(this).addClass('active');
        });
    }

    function initEntityTabs() {
        $('.entity-tabs [data-bs-toggle="tab"]').on('shown.bs.tab', function (event) {
            var key = $(event.target).attr('data-tab-key');
            if (!key || !window.history.replaceState) return;
            var url = new URL(window.location.href);
            url.searchParams.set('tab', key);
            window.history.replaceState({}, '', url.toString());
        });
        var requested = queryParams().get('tab');
        if (!requested) return;
        var tabButton = $('.entity-tabs [data-tab-key="' + requested + '"]').first();
        if (tabButton.length && window.bootstrap && bootstrap.Tab) {
            bootstrap.Tab.getOrCreateInstance(tabButton[0]).show();
        }
    }

    function openSelector(selector) {
        var modal = $(selector);
        if (modal.length) modal.modal('show');
    }

    window.EntityEditor = {
        entityID: entityID,
        isEdit: function (name) { return entityID(name) > 0; },
        queryParams: queryParams,
        parseJSONResponse: parseJSONResponse,
        notify: notify,
        setBusy: setBusy,
        focusField: focusField,
        markDirty: markDirty,
        markClean: markClean,
        save: save,
        bindForm: bindForm,
        openSelector: openSelector,
        init: function () {
            syncEditorSummaryHeight();
            initSectionNav();
            initEntityTabs();
        }
    };

    $(window).on('beforeunload', function (event) {
        if (!dirty || !$('[data-editor-form]').length) return;
        event.preventDefault();
        event.originalEvent.returnValue = '';
    });

    $(window.EntityEditor.init);
})(window, window.jQuery);
