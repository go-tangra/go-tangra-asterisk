CREATE DATABASE asterisk_fixture_cdr;
CREATE DATABASE asterisk_fixture_registration;
CREATE USER 'fixture_reader'@'%' IDENTIFIED BY 'fixture-reader';
GRANT SELECT ON asterisk_fixture_cdr.* TO 'fixture_reader'@'%';
CREATE USER 'fixture_writer'@'%' IDENTIFIED BY 'fixture-writer';
GRANT ALL ON asterisk_fixture_cdr.* TO 'fixture_writer'@'%';
GRANT ALL ON asterisk_fixture_registration.* TO 'fixture_writer'@'%';
