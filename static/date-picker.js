(function () {
    'use strict';

    var instances = [];
    var activePicker = null;
    var uid = 0;

    function pad(value) {
        return String(value).padStart(2, '0');
    }

    function createDate(year, monthIndex, day) {
        return new Date(year, monthIndex, day, 12, 0, 0, 0);
    }

    function toISO(date) {
        return date.getFullYear() + '-' + pad(date.getMonth() + 1) + '-' + pad(date.getDate());
    }

    function parseISO(value) {
        var match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value || '');
        if (!match) return null;

        var year = Number(match[1]);
        var monthIndex = Number(match[2]) - 1;
        var day = Number(match[3]);
        var date = createDate(year, monthIndex, day);

        if (date.getFullYear() !== year || date.getMonth() !== monthIndex || date.getDate() !== day) {
            return null;
        }
        return date;
    }

    function formatChinese(value) {
        var date = parseISO(value);
        if (!date) return '';
        return date.getFullYear() + '年' + pad(date.getMonth() + 1) + '月' + pad(date.getDate()) + '日';
    }

    function sameDate(left, right) {
        return Boolean(left && right &&
            left.getFullYear() === right.getFullYear() &&
            left.getMonth() === right.getMonth() &&
            left.getDate() === right.getDate());
    }

    function clamp(value, min, max) {
        return Math.min(Math.max(value, min), max);
    }

    function CustomDatePicker(input) {
        this.input = input;
        this.wrapper = document.createElement('span');
        this.wrapper.className = 'custom-date-picker';
        this.surface = document.createElement('span');
        this.surface.className = 'custom-date-picker__surface';
        this.valueEl = document.createElement('span');
        this.valueEl.className = 'custom-date-picker__value';
        this.icon = document.createElement('i');
        this.icon.className = 'bi bi-calendar3 custom-date-picker__icon';
        this.icon.setAttribute('aria-hidden', 'true');

        this.surface.appendChild(this.valueEl);
        this.surface.appendChild(this.icon);

        input.parentNode.insertBefore(this.wrapper, input);
        this.wrapper.appendChild(input);
        this.wrapper.appendChild(this.surface);

        input.classList.add('custom-date-picker__native');
        input.setAttribute('autocomplete', 'off');
        input.setAttribute('aria-haspopup', 'dialog');
        input.setAttribute('aria-expanded', 'false');
        input.dataset.customDatePickerReady = 'true';

        this.popup = null;
        this.yearSelect = null;
        this.monthSelect = null;
        this.daysGrid = null;
        this.viewDate = parseISO(input.value) || createDate(new Date().getFullYear(), new Date().getMonth(), 1);
        this.focusOnClose = false;

        this.handlePointerDown = this.handlePointerDown.bind(this);
        this.handleKeyDown = this.handleKeyDown.bind(this);
        this.handleInput = this.handleInput.bind(this);
        this.handleInvalid = this.handleInvalid.bind(this);

        input.addEventListener('pointerdown', this.handlePointerDown);
        input.addEventListener('keydown', this.handleKeyDown);
        input.addEventListener('focus', this.syncDisplay.bind(this));
        input.addEventListener('input', this.handleInput);
        input.addEventListener('change', this.handleInput);
        input.addEventListener('invalid', this.handleInvalid);

        this.syncDisplay();
    }

    CustomDatePicker.prototype.syncDisplay = function () {
        var date = parseISO(this.input.value);
        this.valueEl.textContent = date ? formatChinese(this.input.value) : (this.input.dataset.placeholder || '请选择日期');
        this.wrapper.classList.toggle('is-empty', !date);
        this.surface.title = date ? formatChinese(this.input.value) : '点击选择日期';
    };

    CustomDatePicker.prototype.handleInput = function () {
        this.wrapper.classList.remove('is-invalid');
        this.syncDisplay();
    };

    CustomDatePicker.prototype.handleInvalid = function () {
        this.wrapper.classList.add('is-invalid');
    };

    CustomDatePicker.prototype.handlePointerDown = function (event) {
        event.preventDefault();
        this.input.focus({ preventScroll: true });
        this.open();
    };

    CustomDatePicker.prototype.handleKeyDown = function (event) {
        if (event.key === 'Tab') {
            this.close(false);
            return;
        }
        if (event.key === 'Escape') {
            event.preventDefault();
            this.close(false);
            return;
        }

        if (event.key === 'Enter' || event.key === ' ' || event.key === 'Spacebar' || event.key === 'ArrowDown') {
            event.preventDefault();
            this.open();
            return;
        }

        if (!event.ctrlKey && !event.metaKey && !event.altKey) {
            event.preventDefault();
        }
    };

    CustomDatePicker.prototype.open = function () {
        if (activePicker && activePicker !== this) {
            activePicker.close(false);
        }

        if (!this.popup) {
            this.buildPopup();
        }

        this.syncDisplay();
        this.viewDate = parseISO(this.input.value) || createDate(new Date().getFullYear(), new Date().getMonth(), 1);
        this.render();
        this.position();

        var self = this;
        requestAnimationFrame(function () {
            if (self.popup) self.popup.classList.add('is-open');
        });

        activePicker = this;
        this.wrapper.classList.add('is-open');
        this.input.setAttribute('aria-expanded', 'true');
    };

    CustomDatePicker.prototype.close = function (focusInput) {
        if (this.popup) {
            this.popup.remove();
            this.popup = null;
            this.yearSelect = null;
            this.monthSelect = null;
            this.daysGrid = null;
        }

        this.wrapper.classList.remove('is-open');
        this.input.setAttribute('aria-expanded', 'false');

        if (activePicker === this) activePicker = null;

        if (focusInput) {
            var self = this;
            requestAnimationFrame(function () {
                self.input.focus({ preventScroll: true });
                self.syncDisplay();
            });
        }
    };

    CustomDatePicker.prototype.buildPopup = function () {
        var self = this;
        var popupId = 'custom-date-picker-' + (++uid);
        var popup = document.createElement('div');
        popup.className = 'custom-date-picker-popup';
        popup.id = popupId;
        popup.setAttribute('role', 'dialog');
        popup.setAttribute('aria-label', '选择日期');

        popup.innerHTML = '' +
            '<div class="custom-date-picker-popup__header">' +
                '<button type="button" class="custom-date-picker-popup__nav" data-action="previous" aria-label="上个月"><i class="bi bi-chevron-left"></i></button>' +
                '<div class="custom-date-picker-popup__selectors">' +
                    '<select class="custom-date-picker-popup__select" data-role="year" aria-label="年份"></select>' +
                    '<select class="custom-date-picker-popup__select" data-role="month" aria-label="月份"></select>' +
                '</div>' +
                '<button type="button" class="custom-date-picker-popup__nav" data-action="next" aria-label="下个月"><i class="bi bi-chevron-right"></i></button>' +
            '</div>' +
            '<div class="custom-date-picker-popup__weekdays" aria-hidden="true">' +
                '<span class="custom-date-picker-popup__weekday">一</span>' +
                '<span class="custom-date-picker-popup__weekday">二</span>' +
                '<span class="custom-date-picker-popup__weekday">三</span>' +
                '<span class="custom-date-picker-popup__weekday">四</span>' +
                '<span class="custom-date-picker-popup__weekday">五</span>' +
                '<span class="custom-date-picker-popup__weekday">六</span>' +
                '<span class="custom-date-picker-popup__weekday">日</span>' +
            '</div>' +
            '<div class="custom-date-picker-popup__days" role="grid" aria-label="日期"></div>' +
            '<div class="custom-date-picker-popup__footer">' +
                '<button type="button" class="custom-date-picker-popup__action custom-date-picker-popup__action--today" data-action="today">今天</button>' +
                '<button type="button" class="custom-date-picker-popup__action custom-date-picker-popup__action--clear" data-action="clear">清空</button>' +
            '</div>';

        this.input.setAttribute('aria-controls', popupId);
        this.popup = popup;
        this.yearSelect = popup.querySelector('[data-role="year"]');
        this.monthSelect = popup.querySelector('[data-role="month"]');
        this.daysGrid = popup.querySelector('.custom-date-picker-popup__days');

        popup.addEventListener('click', function (event) {
            var actionButton = event.target.closest('[data-action]');
            if (actionButton) {
                var action = actionButton.dataset.action;
                if (action === 'previous') {
                    self.changeMonth(-1);
                } else if (action === 'next') {
                    self.changeMonth(1);
                } else if (action === 'today') {
                    self.commitValue(toISO(new Date()));
                } else if (action === 'clear') {
                    self.commitValue('');
                }
                return;
            }

            var dayButton = event.target.closest('[data-date]');
            if (dayButton) {
                self.commitValue(dayButton.dataset.date);
            }
        });

        popup.addEventListener('change', function (event) {
            if (event.target === self.yearSelect) {
                self.viewDate.setFullYear(Number(self.yearSelect.value));
                self.render();
            } else if (event.target === self.monthSelect) {
                self.viewDate.setMonth(Number(self.monthSelect.value));
                self.render();
            }
        });

        popup.addEventListener('keydown', function (event) {
            if (event.key === 'Escape') {
                event.preventDefault();
                self.close(true);
                return;
            }

            var dayButton = event.target.closest('[data-date]');
            if (!dayButton) return;

            var offset = 0;
            if (event.key === 'ArrowLeft') offset = -1;
            else if (event.key === 'ArrowRight') offset = 1;
            else if (event.key === 'ArrowUp') offset = -7;
            else if (event.key === 'ArrowDown') offset = 7;
            else return;

            event.preventDefault();
            var current = parseISO(dayButton.dataset.date);
            if (!current) return;
            current.setDate(current.getDate() + offset);
            self.moveFocus(current);
        });

        var host = this.input.closest('.modal') || document.body;
        host.appendChild(popup);
    };

    CustomDatePicker.prototype.render = function () {
        if (!this.popup) return;

        var self = this;
        var selected = parseISO(this.input.value);
        var today = new Date();
        var viewYear = this.viewDate.getFullYear();
        var viewMonth = this.viewDate.getMonth();
        var minimumYear = Math.min(today.getFullYear() - 80, viewYear, selected ? selected.getFullYear() : viewYear);
        var maximumYear = Math.max(today.getFullYear() + 20, viewYear, selected ? selected.getFullYear() : viewYear);

        this.yearSelect.textContent = '';
        for (var year = minimumYear; year <= maximumYear; year++) {
            var yearOption = document.createElement('option');
            yearOption.value = String(year);
            yearOption.textContent = year + ' 年';
            this.yearSelect.appendChild(yearOption);
        }
        this.yearSelect.value = String(viewYear);

        this.monthSelect.textContent = '';
        for (var month = 0; month < 12; month++) {
            var monthOption = document.createElement('option');
            monthOption.value = String(month);
            monthOption.textContent = (month + 1) + ' 月';
            this.monthSelect.appendChild(monthOption);
        }
        this.monthSelect.value = String(viewMonth);

        this.daysGrid.textContent = '';
        var firstDay = createDate(viewYear, viewMonth, 1);
        var mondayOffset = (firstDay.getDay() + 6) % 7;
        var gridStart = createDate(viewYear, viewMonth, 1 - mondayOffset);
        var selectedButton = null;
        var firstInMonthButton = null;

        for (var index = 0; index < 42; index++) {
            var date = createDate(gridStart.getFullYear(), gridStart.getMonth(), gridStart.getDate() + index);
            var iso = toISO(date);
            var button = document.createElement('button');
            button.type = 'button';
            button.className = 'custom-date-picker-popup__day';
            button.dataset.date = iso;
            button.textContent = String(date.getDate());
            button.setAttribute('role', 'gridcell');
            button.setAttribute('aria-label', date.getFullYear() + '年' + (date.getMonth() + 1) + '月' + date.getDate() + '日');
            button.tabIndex = -1;

            if (date.getMonth() !== viewMonth) button.classList.add('is-outside');
            if (sameDate(date, today)) button.classList.add('is-today');
            if (sameDate(date, selected)) {
                button.classList.add('is-selected');
                button.setAttribute('aria-selected', 'true');
                selectedButton = button;
            } else {
                button.setAttribute('aria-selected', 'false');
            }

            if (date.getMonth() === viewMonth && date.getDate() === 1) {
                firstInMonthButton = button;
            }

            this.daysGrid.appendChild(button);
        }

        var initialFocus = selectedButton || firstInMonthButton || this.daysGrid.firstElementChild;
        if (initialFocus) initialFocus.tabIndex = 0;
    };

    CustomDatePicker.prototype.changeMonth = function (amount) {
        this.viewDate = createDate(this.viewDate.getFullYear(), this.viewDate.getMonth() + amount, 1);
        this.render();
    };

    CustomDatePicker.prototype.moveFocus = function (date) {
        if (!this.popup) return;

        if (date.getMonth() !== this.viewDate.getMonth() || date.getFullYear() !== this.viewDate.getFullYear()) {
            this.viewDate = createDate(date.getFullYear(), date.getMonth(), 1);
            this.render();
        }

        var target = this.daysGrid.querySelector('[data-date="' + toISO(date) + '"]');
        if (target) {
            this.daysGrid.querySelectorAll('[data-date]').forEach(function (button) {
                button.tabIndex = -1;
            });
            target.tabIndex = 0;
            target.focus();
        }
    };

    CustomDatePicker.prototype.commitValue = function (value) {
        this.input.value = value || '';
        this.wrapper.classList.remove('is-invalid');
        this.syncDisplay();
        this.input.dispatchEvent(new Event('input', { bubbles: true }));
        this.input.dispatchEvent(new Event('change', { bubbles: true }));
        this.close(true);
    };

    CustomDatePicker.prototype.position = function () {
        if (!this.popup) return;

        var margin = 12;
        var gap = 8;
        var inputRect = this.input.getBoundingClientRect();
        var popupWidth = this.popup.offsetWidth;
        var popupHeight = this.popup.offsetHeight;
        var left = clamp(inputRect.left, margin, Math.max(margin, window.innerWidth - popupWidth - margin));
        var top = inputRect.bottom + gap;

        if (top + popupHeight > window.innerHeight - margin) {
            var above = inputRect.top - popupHeight - gap;
            top = above >= margin ? above : clamp(top, margin, Math.max(margin, window.innerHeight - popupHeight - margin));
        }

        this.popup.style.left = Math.round(left) + 'px';
        this.popup.style.top = Math.round(top) + 'px';
    };

    function enhanceAll(root) {
        var scope = root || document;
        scope.querySelectorAll('input[type="date"]:not([data-custom-date-picker-ready])').forEach(function (input) {
            instances.push(new CustomDatePicker(input));
        });
    }

    function refreshAll() {
        instances.forEach(function (picker) {
            picker.syncDisplay();
        });
    }

    document.addEventListener('pointerdown', function (event) {
        if (!activePicker) return;
        var path = event.composedPath ? event.composedPath() : [];
        if (path.indexOf(activePicker.wrapper) !== -1 || (activePicker.popup && path.indexOf(activePicker.popup) !== -1)) {
            return;
        }
        activePicker.close(false);
    }, true);

    document.addEventListener('keydown', function (event) {
        if (event.key === 'Escape' && activePicker) {
            activePicker.close(true);
        }
    });

    document.addEventListener('shown.bs.modal', function () {
        enhanceAll(document);
        refreshAll();
        if (activePicker) activePicker.close(false);
    });

    document.addEventListener('hidden.bs.modal', function () {
        if (activePicker) activePicker.close(false);
    });

    window.addEventListener('resize', function () {
        if (activePicker) activePicker.position();
    });

    window.addEventListener('scroll', function () {
        if (activePicker) activePicker.position();
    }, true);

    window.CustomDatePicker = {
        enhanceAll: enhanceAll,
        refreshAll: refreshAll
    };

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', function () {
            enhanceAll(document);
        }, { once: true });
    } else {
        enhanceAll(document);
    }
})();