# -*- coding:utf-8 -*-

# Copyright (c) 2012 theo crevon
#
# See the file LICENSE for copying permission.

import sys
import traceback
import zmq
import logging
import procname

from elevator import args
from elevator.db import DatabaseStore
from elevator.config import Config
from elevator.log import setup_loggers
from elevator.backend import Backend
from elevator.frontend import Frontend
from elevator.utils.daemon import Daemon


def setup_process_name(config_file):
    config = ' -c {0} '.format(config_file)
    process_name = 'elevator' + config

    procname.setprocname(process_name)


def log_uncaught_exceptions(e, paranoid=False):
    errors_logger = logging.getLogger("errors_logger")
    tb = traceback.format_exc()

    # Log into errors log
    errors_logger.critical(''.join(tb))
    errors_logger.critical('{0}: {1}'.format(type(e), e.message))

    # Log into stderr
    logging.critical(''.join(tb))
    logging.critical('{0}: {1}'.format(type(e), e.message))

    if paranoid:
        sys.exit(1)


def runserver(config):
    setup_loggers(config)
    activity_logger = logging.getLogger("activity_logger")

    databases = DatabaseStore(config)
    backend = Backend(databases, config)
    frontend = Frontend(config)

    poller = zmq.Poller()
    poller.register(backend.socket, zmq.POLLIN)
    poller.register(frontend.socket, zmq.POLLIN)

    activity_logger.info('Elevator server started on %s' % frontend.host)

    while True:
        try:
            sockets = dict(poller.poll())
            if frontend.socket in sockets:
                if sockets[frontend.socket] == zmq.POLLIN:
                    msg = frontend.socket.recv_multipart(copy=False)
                    backend.socket.send_multipart(msg, copy=False)

            if backend.socket in sockets:
                if sockets[backend.socket] == zmq.POLLIN:
                    msg = backend.socket.recv_multipart(copy=False)
                    frontend.socket.send_multipart(msg, copy=False)
        except KeyboardInterrupt:
            activity_logger.info('Gracefully shuthing down workers')
            del backend
            activity_logger.info('Stopping frontend')
            del frontend
            activity_logger.info('Done')
            return
        except Exception as e:
            log_uncaught_exceptions(e, paranoid=config['paranoid'])


class ServerDaemon(Daemon):
    def run(self, config):
        while True:
            runserver(config)


def main():
    cmdline = args.init_parser().parse_args(sys.argv[1:])
    config = Config(cmdline.config)

    config.update_with_args(cmdline._get_kwargs())
    setup_process_name(cmdline.config)

    if config['daemon'] is True:
        server_daemon = ServerDaemon('/var/run/elevator.pid')
        server_daemon.start()
    else:
        runserver(config)
